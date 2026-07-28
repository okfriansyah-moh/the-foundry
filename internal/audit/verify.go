package audit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
)

type Row struct {
	Seq      int64
	Payload  []byte
	PrevHash []byte
	Hash     []byte
}

type VerifyResult struct {
	RowCount      int
	OK            bool
	BadSeq        int64
	BrokenLinkSeq int64
}

func LoadRows(ctx context.Context, db *sql.DB) ([]Row, error) {
	rows, err := db.QueryContext(ctx, `SELECT seq, payload, prev_hash, hash FROM audit_log ORDER BY seq ASC`)
	if err != nil {
		return nil, fmt.Errorf("audit: query rows: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Row
	for rows.Next() {
		var row Row
		if err := rows.Scan(&row.Seq, &row.Payload, &row.PrevHash, &row.Hash); err != nil {
			return nil, fmt.Errorf("audit: scan row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit: iterate rows: %w", err)
	}
	return out, nil
}

func VerifyRows(rows []Row) VerifyResult {
	result := VerifyResult{OK: true, RowCount: len(rows)}
	var previous []byte
	for i, row := range rows {
		if i > 0 && !bytes.Equal(row.PrevHash, previous) && result.OK {
			result.OK = false
			result.BrokenLinkSeq = row.Seq
		}
		sum := sha256.New()
		sum.Write(row.PrevHash)
		sum.Write(row.Payload)
		if got := sum.Sum(nil); !bytes.Equal(got, row.Hash) && result.OK {
			result.OK = false
			result.BadSeq = row.Seq
		}
		previous = row.Hash
	}
	return result
}

func VerifyChain(ctx context.Context, db *sql.DB) (VerifyResult, error) {
	rows, err := LoadRows(ctx, db)
	if err != nil {
		return VerifyResult{}, err
	}
	return VerifyRows(rows), nil
}
