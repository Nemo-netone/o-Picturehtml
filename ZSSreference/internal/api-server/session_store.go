//  SQLite会话存储：同传会话的持久化CRUD操作
package api

import (
	"context"
	"fmt"
	"log/slog"

	sessionstore "github.com/SATA260/SimulSpeak1/internal/store/sqlite"
)

func OpenSessionStore(ctx context.Context, dsn string, logger *slog.Logger) (*sessionstore.Store, error) {
	store, err := sessionstore.Open(dsn)
	if err != nil {
		return nil, err
	}

	result, err := store.EnsureInitialized(ctx)
	if err != nil {
		closeErr := store.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("initialize sqlite session store: %w; close store: %v", err, closeErr)
		}
		return nil, fmt.Errorf("initialize sqlite session store: %w", err)
	}

	if logger != nil {
		logger.InfoContext(ctx, "SQLite 会话数据库已就绪",
			slog.Bool("alreadyInitialized", result.AlreadyInitialized),
			slog.Bool("migrated", result.Migrated),
			slog.Int("missingTablesBeforeInit", len(result.MissingTables)),
			slog.Int("missingIndexesBeforeInit", len(result.MissingIndexes)),
		)
	}
	return store, nil
}

