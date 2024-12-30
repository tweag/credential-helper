package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"database/sql"

	"github.com/tweag/credential-helper/agent/locate"
	"github.com/tweag/credential-helper/api"
	_ "modernc.org/sqlite"
)

type SqliteCache struct {
	mux sync.RWMutex
	db  *sql.DB
}

func NewSqliteCache() api.Cache {
	dbFilePath := dbPath()
	if dbFilePath != ":memory:" {
		os.MkdirAll(filepath.Dir(dbFilePath), os.ModePerm)
	}
	// TODO: provide a way to close the DB
	db, err := sql.Open("sqlite", dbFilePath)
	if err != nil {
		panic(fmt.Sprintf("failed to open database: %v", err))
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS credentials (cache_key TEXT NOT NULL PRIMARY KEY, get_credentials_response TEXT NOT NULL, expires DATETIME DEFAULT CURRENT_TIMESTAMP)")
	if err != nil {
		panic(fmt.Sprintf("failed to initialize database: %v", err))
	}

	return &SqliteCache{db: db}
}

func (c *SqliteCache) Retrieve(ctx context.Context, cacheKey string) (api.GetCredentialsResponse, error) {
	c.mux.RLock()
	defer c.mux.RUnlock()

	rows, err := c.db.Query("SELECT get_credentials_response FROM credentials WHERE cache_key = ?", cacheKey)
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}
	defer rows.Close()

	var rawGetCredentialsResponse string
	var cachedResponse api.CachableGetCredentialsResponse
	var rowCount int
	for rows.Next() {
		rowCount++
		err := rows.Scan(&rawGetCredentialsResponse)
		if err != nil {
			return api.GetCredentialsResponse{}, err
		}
	}

	if rowCount == 0 {
		return api.GetCredentialsResponse{}, api.CacheMiss
	}
	if rowCount != 1 {
		return api.GetCredentialsResponse{}, fmt.Errorf("database returned %d results for cache key. Expected exactly one", rowCount)
	}

	if err := json.Unmarshal([]byte(rawGetCredentialsResponse), &cachedResponse); err != nil {
		return api.GetCredentialsResponse{}, err
	}

	return cachedResponse.Response, nil
}

func (c *SqliteCache) Store(ctx context.Context, cacheValue api.CachableGetCredentialsResponse) error {
	if len(cacheValue.CacheKey) == 0 || len(cacheValue.Response.Expires) == 0 {
		return nil
	}

	rawGetCredentialsResponse, err := json.Marshal(cacheValue)
	if err != nil {
		return err
	}

	c.mux.Lock()
	defer c.mux.Unlock()

	stmt, err := c.db.Prepare("INSERT OR REPLACE INTO credentials(cache_key, get_credentials_response, expires) VALUES(?, ?, ?)")
	if err != nil {
		return err
	}
	_, err = stmt.Exec(cacheValue.CacheKey, string(rawGetCredentialsResponse), cacheValue.Response.Expires)
	if err != nil {
		return err
	}

	return nil
}

func (c *SqliteCache) Prune(_ context.Context) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	stmt, err := c.db.Prepare("DELETE FROM credentials WHERE expires >= ?")
	if err != nil {
		return err
	}
	_, err = stmt.Exec(time.Now())
	if err != nil {
		return err
	}

	return nil
}

func dbPath() string {
	return locate.LookupPathEnv("CREDENTIAL_HELPER_DB_PATH", filepath.Join("%workdir%", "var", "database.sqlite"), false)
}
