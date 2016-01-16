package sqlite

import (
	"database/sql"
	"database/sql/driver"
	"time"

	"github.com/gravitational/planet/Godeps/_workspace/src/github.com/gravitational/log"
	"github.com/gravitational/planet/Godeps/_workspace/src/github.com/gravitational/trace"
	"github.com/gravitational/planet/Godeps/_workspace/src/github.com/jmoiron/sqlx"
	_ "github.com/gravitational/planet/Godeps/_workspace/src/github.com/mattn/go-sqlite3"
	pb "github.com/gravitational/planet/lib/agent/proto/agentpb"
)

type backend struct {
	*sqlx.DB
	done chan struct{}
}

// TODO: store checkers in a separate table

const schema = `
CREATE TABLE IF NOT EXISTS node (
	id	INTEGER PRIMARY KEY NOT NULL,
	name	TEXT UNIQUE,
	-- active/left/failed
	status	CHAR(1)	CHECK(status IN ('A', 'L', 'F')) NOT NULL DEFAULT 'A'
);

CREATE TABLE IF NOT EXISTS checker (
	id   INTEGER PRIMARY KEY NOT NULL,
	name TEXT UNIQUE,
	desc TEXT
);

-- composite ID: (node, checker, captured_at)
CREATE TABLE IF NOT EXISTS probe (
	node	    INTEGER NOT NULL,
	checker	    TEXT NOT NULL,
	extra 	    TEXT,
	-- running/failed/terminated
	status	    CHAR(1) CHECK(status IN ('H', 'F', 'T')) NOT NULL DEFAULT 'F',
	error	    TEXT NOT NULL,
	captured_at TIMESTAMP NOT NULL
);
`

// New creates a new sqlite backend using the specified file.
func New(path string) (*backend, error) {
	db, err := sqlx.Open("sqlite3", path)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return newBackend(db)
}

// UpdateNode will update the status of the node specified by status.
func (r *backend) UpdateNode(status *pb.NodeStatus) (err error) {
	err = r.inTx(func(tx *sqlx.Tx) error {
		var id int64
		id, err = r.upsertNode(status.Name)
		if err != nil {
			return trace.Wrap(err)
		}
		err = r.addStatus(id, status)
		if err != nil {
			return trace.Wrap(err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// RecentStatus retrieves the last few status records for the specified node.
func (r *backend) RecentStatus(node string) ([]*pb.Probe, error) {
	const selectStmt = `
	SELECT p.checker, p.extra, p.status, p.error, p.captured_at
	FROM probe p JOIN node n WHERE p.node = n.id AND n.name = ?
	ORDER BY p.captured_at DESC
	LIMIT 5
	`
	rows, err := r.Query(selectStmt, node)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	defer rows.Close()

	var probes []*pb.Probe
	for rows.Next() {
		probe := pb.Probe{}
		var when timestamp
		var status string
		err = rows.Scan(&probe.Checker, &probe.Extra, &status, &probe.Error, &when)
		if err != nil {
			return nil, trace.Wrap(err)
		}
		probe.Timestamp = (*pb.Timestamp)(&when)
		probe.Status = serviceStatus(status).toProto()
		probes = append(probes, &probe)
	}
	err = rows.Err()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return probes, nil
}

// Close closes the database.
func (r *backend) Close() error {
	close(r.done)
	return r.DB.Close()
}

func (r *backend) upsertNode(node string) (id int64, err error) {
	const insertStmt = `INSERT OR IGNORE INTO node(name) VALUES(?)`
	var res sql.Result
	res, err = r.Exec(insertStmt, node)
	if err != nil {
		return 0, trace.Wrap(err)
	}
	id, _ = res.LastInsertId()
	if id == 0 {
		err = r.Get(&id, `SELECT id FROM node WHERE name=?`, node)
		if err != nil {
			return 0, trace.Wrap(err)
		}
	}
	return id, nil
}

func (r *backend) addStatus(node int64, status *pb.NodeStatus) (err error) {
	const insertStmt = `
		INSERT INTO probe(node, checker, extra, status, error, captured_at)
		VALUES(?, ?, ?, ?, ?, ?)
	`
	for _, probe := range status.Probes {
		_, err = r.Exec(insertStmt, node, probe.Checker, probe.Extra, protoToStatus(probe.Status),
			probe.Error, timestamp(*probe.Timestamp))
		if err != nil {
			return trace.Wrap(err)
		}
	}
	return nil
}

type serviceStatus string

func protoToStatus(status pb.ServiceStatusType) serviceStatus {
	switch status {
	case pb.ServiceStatusType_ServiceRunning:
		return serviceStatus("H")
	case pb.ServiceStatusType_ServiceTerminated:
		return serviceStatus("T")
	case pb.ServiceStatusType_ServiceFailed:
		fallthrough
	default:
		return serviceStatus("F")
	}
}

func (s serviceStatus) toProto() pb.ServiceStatusType {
	switch s {
	case "H":
		return pb.ServiceStatusType_ServiceRunning
	case "T":
		return pb.ServiceStatusType_ServiceTerminated
	case "F":
		fallthrough
	default:
		return pb.ServiceStatusType_ServiceFailed
	}
}

// driver.Valuer
func (s serviceStatus) Value() (value driver.Value, err error) {
	return string(s), nil
}

type timestamp pb.Timestamp

// sql.Scanner
func (ts *timestamp) Scan(src interface{}) error {
	return (*pb.Timestamp)(ts).UnmarshalText(src.([]byte))
}

// driver.Valuer
func (ts timestamp) Value() (value driver.Value, err error) {
	return pb.Timestamp(ts).MarshalText()
}

const scavengeTimeout = 24 * time.Hour

func (r *backend) scavengeLoop() {
	for {
		select {
		case <-time.After(scavengeTimeout):
			if err := r.deleteOlderThan(time.Now().Add(-scavengeTimeout)); err != nil {
				log.Errorf("failed to scavenge stats: %v", err)
			}
		case <-r.done:
			return
		}
	}
}

func (r *backend) deleteOlderThan(limit time.Time) error {
	const deleteStmt = `DELETE FROM probe WHERE captured_at < ?`

	err := r.inTx(func(tx *sqlx.Tx) error {
		_, err := r.Exec(deleteStmt, limit)
		if err != nil {
			return trace.Wrap(err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

type cleanup func()

func (r *backend) inTx(f func(tx *sqlx.Tx) error) error {
	tx, err := r.Beginx()
	if err != nil {
		return trace.Wrap(err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
			return
		}
		tx.Commit()
	}()
	err = f(tx)
	if err != nil {
		return trace.Wrap(err)
	}
	return nil
}

func newInMemory() (*backend, error) {
	db, err := sqlx.Open("sqlite3", ":memory:")
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return newBackend(db)
}

func newBackend(db *sqlx.DB) (*backend, error) {
	_, err := db.Exec(schema)
	if err != nil {
		return nil, trace.Wrap(err, "failed to create schema")
	}
	backend := &backend{
		DB:   db,
		done: make(chan struct{}),
	}
	go backend.scavengeLoop()
	return backend, nil
}
