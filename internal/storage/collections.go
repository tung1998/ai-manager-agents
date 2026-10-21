package storage

import (
	"context"
	"time"
)

// Data collections (ADR-172): structured data a project keeps without code.

// Field types of a collection.
const (
	FieldText         = "text"
	FieldNumber       = "number"
	FieldBool         = "bool"
	FieldDate         = "date" // RFC 3339 or 2006-01-02
	FieldEnum         = "enum" // one of Options
	FieldTags         = "tags" // a list of strings
	FieldRef          = "ref"  // a record of collection Ref, same project
	FieldConversation = "conversation"
	FieldURL          = "url"
	FieldFile         = "file" // a path in the project or an attachment id
)

// Field is one field of a collection.
type Field struct {
	Name     string   `json:"name"`
	Label    string   `json:"label,omitempty"`
	Type     string   `json:"type"`
	Options  []string `json:"options,omitempty"` // enum
	Ref      string   `json:"ref,omitempty"`     // ref: the collection's key
	Required bool     `json:"required,omitempty"`
	Unique   bool     `json:"unique,omitempty"`  // one record per value (an upsert matches on it)
	Indexed  bool     `json:"indexed,omitempty"` // filtered and sorted fast
}

// Collection is a typed set of records in a project.
type Collection struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Pack        string    `json:"pack"` // the feature pack that declared it ("" = the project's own)
	Fields      []Field   `json:"fields"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Record is one entry of a collection.
type Record struct {
	ID           string         `json:"id"`
	CollectionID string         `json:"collection_id"`
	ProjectID    string         `json:"project_id"`
	Data         map[string]any `json:"data"`
	Text         string         `json:"-"` // what search reads
	CreatedBy    string         `json:"created_by"`
	UpdatedBy    string         `json:"updated_by"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// RecordChange is one change of a record: what it was after (create,
// update) or before (delete).
type RecordChange struct {
	ID       string         `json:"id"`
	RecordID string         `json:"record_id"`
	Op       string         `json:"op"` // create | update | delete
	Data     map[string]any `json:"data"`
	By       string         `json:"by"`
	At       time.Time      `json:"at"`
}

// Cond is one filter: a field compared with a value.
type Cond struct {
	Field string `json:"field"`
	Op    string `json:"op"` // eq ne lt lte gt gte contains in
	Value any    `json:"value"`
}

// RecordQuery picks the records of a collection.
type RecordQuery struct {
	CollectionID string
	Where        []Cond // fields already checked against the schema
	Search       string // words, accents ignored
	Sort         string // a field, or created_at / updated_at ("" = updated_at)
	Desc         bool
	Limit        int
	Offset       int
}

// CollectionRepo keeps collections and their records.
type CollectionRepo interface {
	Create(ctx context.Context, c Collection) (Collection, error) // ErrConflict: the key is taken
	Update(ctx context.Context, c Collection) error               // name, description, fields
	Get(ctx context.Context, projectID, key string) (Collection, error)
	ByID(ctx context.Context, id string) (Collection, error)
	List(ctx context.Context, projectID string) ([]Collection, error)
	// Index makes the indexes of c's indexed and unique fields (a unique
	// index fails, ErrConflict, when records already share a value).
	Index(ctx context.Context, c Collection) error

	Insert(ctx context.Context, r Record) (Record, error) // ErrConflict: a unique field's value is taken
	Save(ctx context.Context, r Record) error             // data and text, with its history
	Delete(ctx context.Context, r Record, by string) error
	Record(ctx context.Context, collectionID, id string) (Record, error)
	FindBy(ctx context.Context, collectionID, field string, value any) (Record, error)
	Query(ctx context.Context, q RecordQuery) ([]Record, int, error) // the page, and how many match
	Count(ctx context.Context, projectID string) (int, error)
	History(ctx context.Context, recordID string, limit int) ([]RecordChange, error)
}
