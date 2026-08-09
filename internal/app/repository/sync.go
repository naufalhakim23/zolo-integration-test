package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"zolo-test-integration/internal/app/repository/model"
	"zolo-test-integration/internal/pkg"
)

// PendingReclaimAfter is how long a PENDING claim may sit before another request may
// take it over.
const PendingReclaimAfter = 2 * time.Minute

type (
	ISyncRepository interface {
		ClaimAttempt(ctx context.Context, orderID, tenantID, idempotencyKey string) (attempt model.SyncAttempt, claimed bool, err error)
		CompleteAttempt(ctx context.Context, attempt model.SyncAttempt) error
		GetAttemptByKey(ctx context.Context, idempotencyKey string) (model.SyncAttempt, error)
		GetLatestAttemptByOrderID(ctx context.Context, orderID string) (model.SyncAttempt, error)
	}

	SyncRepository struct {
		RepositoryOption
	}
)

func InitiateSyncRepository(opt RepositoryOption) ISyncRepository {
	return &SyncRepository{RepositoryOption: opt}
}

// ClaimAttempt takes ownership of a sync, reporting claimed=false when someone else
// already owns it. Write idemkey enforced
func (r *SyncRepository) ClaimAttempt(ctx context.Context, orderID, tenantID, idempotencyKey string) (model.SyncAttempt, bool, error) {
	insert := fmt.Sprintf(`
	INSERT INTO %s (order_id, tenant_id, idempotency_key, status, attempt_count)
	VALUES (?, ?, ?, ?, 1)
	ON CONFLICT (idempotency_key) DO NOTHING`, TableSyncAttempts)

	res, err := r.DB.ExecContext(ctx, r.DB.Rebind(insert), orderID, tenantID, idempotencyKey, pkg.StatusPending)
	if err != nil {
		return model.SyncAttempt{}, false, pkg.NewDatabaseError(err)
	}

	if rows, _ := res.RowsAffected(); rows == 1 {
		attempt, err := r.GetAttemptByKey(ctx, idempotencyKey)
		return attempt, true, err
	}

	// The predicates live in the UPDATE rather than a read-then-write, so two requests
	// racing to reclaim the same row cannot both win.
	reclaim := fmt.Sprintf(`
	UPDATE %s
	SET status        = ?,
	    attempt_count = attempt_count + 1,
	    updated_at    = CURRENT_TIMESTAMP
	WHERE idempotency_key = ?
	  AND (
	        (status = ? AND updated_at <= ?)
	     OR status IN (?, ?)
	  )`, TableSyncAttempts)

	staleBefore := time.Now().UTC().Add(-PendingReclaimAfter).Format(time.DateTime)

	res, err = r.DB.ExecContext(ctx, r.DB.Rebind(reclaim),
		pkg.StatusPending,
		idempotencyKey,
		pkg.StatusPending, staleBefore,
		pkg.StatusFailed, pkg.StatusPartialSuccess,
	)
	if err != nil {
		return model.SyncAttempt{}, false, pkg.NewDatabaseError(err)
	}
	reclaimed, _ := res.RowsAffected()

	attempt, err := r.GetAttemptByKey(ctx, idempotencyKey)
	if err != nil {
		return model.SyncAttempt{}, false, err
	}

	return attempt, reclaimed == 1, nil
}

// CompleteAttempt writes the terminal state and its per-line detail in one
// transaction, so the dashboard never reads a SYNCED order whose line results are
// still missing.
func (r *SyncRepository) CompleteAttempt(ctx context.Context, attempt model.SyncAttempt) error {
	update := fmt.Sprintf(`
	UPDATE %s
	SET status            = ?,
	    error_code        = ?,
	    message_code      = ?,
	    message_params    = ?,
	    request_snapshot  = ?,
	    response_snapshot = ?,
	    updated_at        = CURRENT_TIMESTAMP
	WHERE id = ?`, TableSyncAttempts)

	deleteLines := fmt.Sprintf(`DELETE FROM %s WHERE sync_attempt_id = ?`, TableSyncLineResults)

	insertLine := fmt.Sprintf(`
	INSERT INTO %s (sync_attempt_id, sku, accepted, reason)
	VALUES (?, ?, ?, ?)`, TableSyncLineResults)

	return TransactionWrapper(ctx, r.DB, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, tx.Rebind(update),
			attempt.Status,
			attempt.ErrorCode,
			attempt.MessageCode,
			attempt.MessageParams,
			attempt.RequestSnapshot,
			attempt.ResponseSnapshot,
			attempt.ID,
		)
		if err != nil {
			return pkg.NewDatabaseError(err)
		}

		// A reclaimed attempt still carries line results from the run that died.
		// Replacing them keeps the audit trail consistent with the response snapshot.
		if _, err := tx.ExecContext(ctx, tx.Rebind(deleteLines), attempt.ID); err != nil {
			return pkg.NewDatabaseError(err)
		}

		for _, line := range attempt.Lines {
			if _, err := tx.ExecContext(ctx, tx.Rebind(insertLine), attempt.ID, line.SKU, line.Accepted, line.Reason); err != nil {
				return pkg.NewDatabaseError(err)
			}
		}

		return nil
	})
}

func (r *SyncRepository) GetAttemptByKey(ctx context.Context, idempotencyKey string) (docs model.SyncAttempt, err error) {
	query := fmt.Sprintf(`
	SELECT *
	FROM %s
	WHERE idempotency_key = ?`, TableSyncAttempts)

	if err = r.DB.GetContext(ctx, &docs, r.DB.Rebind(query), idempotencyKey); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return docs, pkg.NewNotFoundError(pkg.MsgOrderNotFound, err)
		}
		return docs, pkg.NewDatabaseError(err)
	}

	docs.Lines, err = r.getLines(ctx, docs.ID)
	return docs, err
}

func (r *SyncRepository) GetLatestAttemptByOrderID(ctx context.Context, orderID string) (docs model.SyncAttempt, err error) {
	query := fmt.Sprintf(`
	SELECT *
	FROM %s
	WHERE order_id = ?
	ORDER BY id DESC
	LIMIT 1`, TableSyncAttempts)

	if err = r.DB.GetContext(ctx, &docs, r.DB.Rebind(query), orderID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return docs, pkg.NewNotFoundError(pkg.MsgOrderNotFound, err).
				WithParam("order_id", orderID)
		}
		return docs, pkg.NewDatabaseError(err)
	}

	docs.Lines, err = r.getLines(ctx, docs.ID)
	return docs, err
}

func (r *SyncRepository) getLines(ctx context.Context, attemptID int64) (docs []model.SyncLineResult, err error) {
	query := fmt.Sprintf(`
	SELECT *
	FROM %s
	WHERE sync_attempt_id = ?
	ORDER BY id`, TableSyncLineResults)

	if err = r.DB.SelectContext(ctx, &docs, r.DB.Rebind(query), attemptID); err != nil {
		return nil, pkg.NewDatabaseError(err)
	}

	return docs, nil
}
