package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// LockDelivery excludes sending and identity decisions for one submission, even
// across local processes, without holding a SQLite transaction during SMTP.
// Acquire before BEGIN IMMEDIATE. OS locks are released automatically on crash.
func LockDelivery(ctx context.Context, db *sql.DB, id int32, wait bool) (func(), bool, error) {
	path, err := Path(ctx, db)
	if err != nil {
		return nil, false, err
	}
	dir := path + ".locks"
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, false, err
	}
	lock := flock.New(filepath.Join(dir, fmt.Sprintf("delivery-%d.lock", id)))
	var ok bool
	if wait {
		ok, err = lock.TryLockContext(ctx, 10*time.Millisecond)
	} else {
		ok, err = lock.TryLock()
	}
	if err != nil || !ok {
		lock.Close()
		return nil, ok, err
	}
	return func() { _ = lock.Close() }, true, nil
}
