package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"zolo-test-integration/internal/app/repository/model"
	"zolo-test-integration/internal/pkg"
)

type (
	ISyncRepository interface {
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

func (r *SyncRepository) GetAttemptByKey(ctx context.Context, idempotencyKey string) (docs model.SyncAttempt, err error) {
	query := fmt.Sprintf(`
	SELECT * 
	FROM %s 
	WHERE idempotency_key = %s`, TableSyncAttempts, r.DB.Rebind("?"))

	if err = r.DB.GetContext(ctx, &docs, query, idempotencyKey); err != nil {
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
	WHERE order_id = %s 
	ORDER BY id DESC 
	LIMIT 1`, TableSyncAttempts, r.DB.Rebind("?"))

	if err = r.DB.GetContext(ctx, &docs, query, orderID); err != nil {
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
	WHERE sync_attempt_id = %s
	ORDER BY line_no`, TableSyncLineResults, r.DB.Rebind("?"))

	if err = r.DB.SelectContext(ctx, &docs, query, attemptID); err != nil {
		return nil, pkg.NewDatabaseError(err)
	}

	return docs, nil
}
