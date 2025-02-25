package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/jmoiron/sqlx"
	"modernc.org/sqlite"
	_ "modernc.org/sqlite"
)

type Storage interface {
	StoreFingerprints(fp map[Address]Couple) error
	GetCouples([]Address) (map[Address][]Couple, error)
	GetSongByID(uint32) (Song, bool, error)
	RegisterSong(key string, metadata string) (uint32, error)
}

type Address uint32

type Couple struct {
	AnchorTimeMs uint32
	SongID       uint32
}

type SQLiteClient struct {
	db *sqlx.DB
	mu sync.RWMutex
}

func NewSQLiteClient(dataSourceName string) (*SQLiteClient, error) {
	db, err := sqlx.Open("sqlite", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("error connecting to SQLite: %s", err)
	}

	err = createTables(db)
	if err != nil {
		return nil, fmt.Errorf("error creating tables: %s", err)
	}

	return &SQLiteClient{db: db}, nil
}

// createTables creates the required tables if they don't exist
func createTables(db *sqlx.DB) error {
	createSongsTable := `
    CREATE TABLE IF NOT EXISTS songs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		song TEXT NOT NULL,
		key TEXT NOT NULL UNIQUE
    );
    `

	createFingerprintsTable := `
    CREATE TABLE IF NOT EXISTS fingerprints (
        address INTEGER NOT NULL,
        anchorTimeMs INTEGER NOT NULL,
        songID INTEGER NOT NULL,
        PRIMARY KEY (address, anchorTimeMs, songID)
    );
    `

	_, err := db.Exec(createSongsTable)
	if err != nil {
		return fmt.Errorf("error creating songs table: %s", err)
	}

	_, err = db.Exec(createFingerprintsTable)
	if err != nil {
		return fmt.Errorf("error creating fingerprints table: %s", err)
	}

	return nil
}

func (db *SQLiteClient) Close() error {
	if db.db != nil {
		return db.db.Close()
	}
	return nil
}

func (db *SQLiteClient) StoreFingerprints(fingerprints map[Address]Couple) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	tx, err := db.db.Beginx()
	if err != nil {
		return fmt.Errorf("error starting transaction: %s", err)
	}
	defer tx.Rollback()

	query := `INSERT OR REPLACE INTO fingerprints (address, anchorTimeMs, songID) VALUES (?, ?, ?)`
	for address, couple := range fingerprints {
		//fmt.Println(address, couple.AnchorTimeMs, couple.SongID)
		if _, err := tx.Exec(query, address, couple.AnchorTimeMs, couple.SongID); err != nil {
			return fmt.Errorf("error executing statement: %w", err)
		}
	}

	return tx.Commit()
}

func (db *SQLiteClient) GetCouples(addresses []Address) (map[Address][]Couple, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	couples := make(map[Address][]Couple)

	for _, address := range addresses {
		rows, err := db.db.Query("SELECT anchorTimeMs, songID FROM fingerprints WHERE address = ?", address)
		if err != nil {
			return nil, fmt.Errorf("error querying database: %s", err)
		}
		defer rows.Close()

		var docCouples []Couple
		for rows.Next() {
			var couple Couple
			if err := rows.Scan(&couple.AnchorTimeMs, &couple.SongID); err != nil {
				return nil, fmt.Errorf("error scanning row: %s", err)
			}
			docCouples = append(docCouples, couple)
		}
		couples[address] = docCouples
	}

	return couples, nil
}

func (db *SQLiteClient) RegisterSong(key string, metadata string) (uint32, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	tx, err := db.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("error starting transaction: %s", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec("INSERT INTO songs (song, key) VALUES (?, ?);", metadata, key)
	if err != nil {
		var sqlerr *sqlite.Error
		if errors.As(err, &sqlerr) && (sqlerr.Code() == 2067 || sqlerr.Code() == 1555) {
			return 0, nil
		}
		return 0, fmt.Errorf("error executing statement: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("error getting last insert id: %w", err)
	}

	return uint32(id), tx.Commit()
}

// GetSong retrieves a song by filter key
func (s *SQLiteClient) GetSongByID(songId uint32) (Song, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT id, song, key FROM songs WHERE id = ?"

	row := s.db.QueryRow(query, songId)

	var song Song
	err := row.Scan(&song.ID, &song.Metadata, &song.Key)
	if err != nil {
		if err == sql.ErrNoRows {
			return Song{}, false, nil
		}
		return Song{}, false, fmt.Errorf("failed to retrieve song: %s", err)
	}

	return song, true, nil
}

type Song struct {
	ID       uint32
	Key      string
	Metadata string
}
