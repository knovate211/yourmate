package main

import (
	"context"
	"embed"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
)

// campus_directory.csv is generated from the campus master list by
// scripts/campus-directory/convert.py.
//
//go:embed data/campus_directory.csv
var directoryFiles embed.FS

type directoryEntry struct {
	name, country, city, timezone, currency, website string
}

func readDirectory() ([]directoryEntry, error) {
	f, err := directoryFiles.Open("data/campus_directory.csv")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	col := map[string]int{}
	for i, h := range header {
		col[h] = i
	}
	for _, want := range []string{"name", "country", "city", "timezone", "currency", "website"} {
		if _, ok := col[want]; !ok {
			return nil, fmt.Errorf("campus directory: missing column %q", want)
		}
	}
	var out []directoryEntry
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		e := directoryEntry{
			name:     strings.TrimSpace(rec[col["name"]]),
			country:  strings.TrimSpace(rec[col["country"]]),
			city:     strings.TrimSpace(rec[col["city"]]),
			timezone: strings.TrimSpace(rec[col["timezone"]]),
			currency: strings.ToUpper(strings.TrimSpace(rec[col["currency"]])),
			website:  strings.TrimSpace(rec[col["website"]]),
		}
		if e.name == "" {
			continue
		}
		if _, err := time.LoadLocation(e.timezone); err != nil || !currencyRe.MatchString(e.currency) {
			return nil, fmt.Errorf("campus directory: %q has an invalid timezone or currency", e.name)
		}
		out = append(out, e)
	}
	return out, nil
}

// importDirectory adds every directory campus that doesn't exist yet,
// matching case-insensitively on the current name or the original directory
// name. It runs at every start and is a no-op once the directory is loaded,
// so editing, renaming or rejecting a campus is never undone by a restart.
func (s *server) importDirectory(ctx context.Context) error {
	entries, err := readDirectory()
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('campus-directory'))`); err != nil {
		return err
	}

	names := map[string]bool{}
	slugs := map[string]bool{}
	rows, err := tx.Query(ctx, `SELECT lower(coalesce(directory_name, name)), lower(name), slug FROM campuses`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var dir, n, sl string
		if err := rows.Scan(&dir, &n, &sl); err != nil {
			rows.Close()
			return err
		}
		names[dir], names[n], slugs[sl] = true, true, true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	added := 0
	for _, e := range entries {
		if names[strings.ToLower(e.name)] {
			continue
		}
		slug := slugify(e.name)
		for i := 2; slugs[slug]; i++ {
			slug = fmt.Sprintf("%s-%d", slugify(e.name), i)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO campuses (slug, name, country, city, timezone, currency, website, status, source, directory_name)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', 'directory', $2)`,
			slug, e.name, e.country, e.city, e.timezone, e.currency, e.website); err != nil {
			return fmt.Errorf("import %q: %w", e.name, err)
		}
		names[strings.ToLower(e.name)], slugs[slug] = true, true
		added++
	}
	// Campuses that existed before the import (the hand-picked starters)
	// remember their directory name too, so renaming them is safe.
	all := make([]string, len(entries))
	for i, e := range entries {
		all[i] = e.name
	}
	if _, err := tx.Exec(ctx, `UPDATE campuses c SET directory_name = d.name
		FROM unnest($1::text[]) AS d(name)
		WHERE c.directory_name IS NULL
		  AND c.id = (SELECT x.id FROM campuses x WHERE lower(x.name) = lower(d.name)
		              ORDER BY x.status = 'active' DESC, x.created_at LIMIT 1)
		  AND NOT EXISTS (SELECT 1 FROM campuses y WHERE lower(y.directory_name) = lower(d.name))`, all); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if added > 0 {
		slog.Info("imported campus directory", "added", added, "in_file", len(entries))
	}
	return nil
}
