package repository

type (
	ISyncRepository interface {
	}

	SyncRepository struct {
		RepositoryOption
	}
)

func InitiateSyncRepository(opt RepositoryOption) ISyncRepository {
	return &SyncRepository{RepositoryOption: opt}
}
