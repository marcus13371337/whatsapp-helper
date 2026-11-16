package wa

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "modernc.org/sqlite"
)

type keyStore struct {
	db        *sql.DB
	container *sqlstore.Container
}

type sqliteImage interface {
	Serialize() ([]byte, error)
	Deserialize([]byte) error
}

func openKeyStore(ctx context.Context, encryption string, log zerolog.Logger) (*keyStore, error) {
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	ks := &keyStore{db: db}
	if encryption != "" {
		if err := ks.restore(ctx, encryption); err != nil {
			db.Close()
			return nil, err
		}
	}
	ks.container = sqlstore.NewWithDB(db, "sqlite", waLog.Zerolog(log))
	if err := ks.container.Upgrade(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("prepare key store: %w", err)
	}
	return ks, nil
}

func (ks *keyStore) withImage(ctx context.Context, fn func(sqliteImage) error) error {
	conn, err := ks.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Raw(func(raw any) error {
		image, ok := raw.(sqliteImage)
		if !ok {
			return driver.ErrSkip
		}
		return fn(image)
	})
}

func (ks *keyStore) restore(ctx context.Context, encryption string) error {
	packed, err := base64.StdEncoding.DecodeString(encryption)
	if err != nil {
		return fmt.Errorf("decode encryption state: %w", err)
	}
	unpacker, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return fmt.Errorf("unpack encryption state: %w", err)
	}
	image, err := io.ReadAll(unpacker)
	if err != nil {
		return fmt.Errorf("unpack encryption state: %w", err)
	}
	return ks.withImage(ctx, func(db sqliteImage) error { return db.Deserialize(image) })
}

func (ks *keyStore) snapshot(ctx context.Context) (string, error) {
	var image []byte
	err := ks.withImage(ctx, func(db sqliteImage) (err error) {
		image, err = db.Serialize()
		return err
	})
	if err != nil {
		return "", err
	}
	var packed bytes.Buffer
	packer := gzip.NewWriter(&packed)
	if _, err := packer.Write(image); err != nil {
		return "", err
	}
	if err := packer.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(packed.Bytes()), nil
}

func (ks *keyStore) device(ctx context.Context) (*store.Device, error) {
	return ks.container.GetFirstDevice(ctx)
}

func (ks *keyStore) close() {
	ks.db.Close()
}
