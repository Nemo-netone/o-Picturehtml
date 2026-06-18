//  SQLite会话存储单元测试
package api_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	apiapp "github.com/SATA260/SimulSpeak1/internal/api-server"
)

func TestOpenSessionStoreInitializesSQLiteSchema(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	dsn := t.TempDir() + "/simulspeak.db"

	store, err := apiapp.OpenSessionStore(ctx, dsn, logger)
	if err != nil {
		t.Fatalf("open api session store: %v", err)
	}

	status, err := store.InitializationStatus(ctx)
	if err != nil {
		t.Fatalf("inspect api session store: %v", err)
	}
	if !status.Initialized {
		t.Fatalf("api session store should initialize schema on startup: %#v", status)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close api session store: %v", err)
	}

	store, err = apiapp.OpenSessionStore(ctx, dsn, logger)
	if err != nil {
		t.Fatalf("reopen api session store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close reopened api session store: %v", err)
		}
	})

	status, err = store.InitializationStatus(ctx)
	if err != nil {
		t.Fatalf("inspect reopened api session store: %v", err)
	}
	if !status.Initialized {
		t.Fatalf("api session store should stay initialized across restarts: %#v", status)
	}
}

