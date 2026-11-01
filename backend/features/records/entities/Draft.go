// Package entities keeps entity types editable by many people at once
// (documentation/Records.md › Built in slice 16): an entity type's draft is
// an aggregate on the def stream, changed only by entity.* commands and
// folded into the records.entity projection; entity.publish commits the
// draft as a version, which the records.entityPublished projection keeps
// and the versions follower copies into the tenant database. The
// entity.describe and entity.list queries are what editors and the chat
// read.
package entities

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/SgtFlame/foundation/backend/features/live"
	"github.com/SgtFlame/foundation/backend/features/records"
)

// TargetType is an entity type's target type on the def stream.
const TargetType = "entity"

// Projector IDs.
const (
	DraftProjector     = "records.entity"          // each entity type's draft
	PublishedProjector = "records.entityPublished" // each entity type's last published version
)

// Draft is the records.entity projection's value: an entity type as it is
// being edited.
type Draft struct {
	Entity    records.EntityType `json:"entity"`
	Rev       int                `json:"rev"`       // events folded; entity.publish names the one its sender saw
	Published int                `json:"published"` // the last published version, 0 before the first
	// PublishedRev is the Rev the last version was published at, so an
	// editor can tell whether the draft has changed since.
	PublishedRev int       `json:"publishedRev"`
	UpdatedBy    uint64    `json:"updatedBy,string"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Events an entity type's commands commit.
const (
	EventCreated       = "created"       // {entity}
	EventInfoSet       = "infoSet"       // Info
	EventFieldAdded    = "fieldAdded"    // {field}
	EventFieldUpdated  = "fieldUpdated"  // {field}: the whole field as it now is
	EventFieldRemoved  = "fieldRemoved"  // {field: id}
	EventFieldRestored = "fieldRestored" // {field: id}
	EventIndexAdded    = "indexAdded"    // {index}
	EventIndexRemoved  = "indexRemoved"  // {index: id}
	EventAccessSet     = "accessSet"     // {access}
	EventPublished     = "published"     // Publication
)

// Info changes an entity type's name, description, owner and title field;
// nil leaves them.
type Info struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Owner       *string `json:"owner,omitempty"`
	TitleField  *string `json:"titleField,omitempty"`
}

// Publication is a published version.
type Publication struct {
	Version int                `json:"version"`
	Entity  records.EntityType `json:"entity"`
}

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	err := json.Unmarshal(raw, &v)
	return v, err
}

// Apply folds one event into a draft. Commands also apply the event they
// decide to a copy, and check the result.
func Apply(d *Draft, e live.Event) error {
	x := &d.Entity
	switch e.Type {
	case EventCreated:
		v, err := decode[struct{ Entity records.EntityType }](e.Data)
		if err != nil {
			return err
		}
		*x = v.Entity
	case EventInfoSet:
		in, err := decode[Info](e.Data)
		if err != nil {
			return err
		}
		if in.Name != nil {
			x.Name = *in.Name
		}
		if in.Description != nil {
			x.Description = *in.Description
		}
		if in.Owner != nil {
			x.Owner = *in.Owner
		}
		if in.TitleField != nil {
			x.TitleField = *in.TitleField
		}
	case EventFieldAdded:
		v, err := decode[struct{ Field records.Field }](e.Data)
		if err != nil {
			return err
		}
		x.Fields = append(x.Fields, v.Field)
	case EventFieldUpdated:
		v, err := decode[struct{ Field records.Field }](e.Data)
		if err != nil {
			return err
		}
		if _, i, ok := x.FieldByID(v.Field.ID); ok {
			x.Fields[i] = v.Field
		}
	case EventFieldRemoved, EventFieldRestored:
		v, err := decode[struct{ Field string }](e.Data)
		if err != nil {
			return err
		}
		if _, i, ok := x.FieldByID(v.Field); ok {
			x.Fields[i].Hidden = e.Type == EventFieldRemoved
		}
	case EventIndexAdded:
		v, err := decode[struct{ Index records.Index }](e.Data)
		if err != nil {
			return err
		}
		x.Indexes = append(x.Indexes, v.Index)
	case EventIndexRemoved:
		v, err := decode[struct{ Index string }](e.Data)
		if err != nil {
			return err
		}
		x.Indexes = slices.DeleteFunc(x.Indexes, func(i records.Index) bool { return i.ID == v.Index })
	case EventAccessSet:
		v, err := decode[struct{ Access records.Access }](e.Data)
		if err != nil {
			return err
		}
		x.Access = v.Access
	case EventPublished:
		p, err := decode[Publication](e.Data)
		if err != nil {
			return err
		}
		d.Published, d.PublishedRev, x.Version = p.Version, d.Rev+1, p.Version
	default:
		return fmt.Errorf("unknown event %s", e.Type)
	}
	d.Rev++
	d.UpdatedBy, d.UpdatedAt = e.UserID, e.Time
	x.ID = e.Target.ID
	if x.Fields == nil {
		x.Fields = []records.Field{}
	}
	if x.Indexes == nil {
		x.Indexes = []records.Index{}
	}
	return nil
}

// fold is the records.entity projector's fold.
func fold(f live.Fold) (json.RawMessage, error) {
	var d Draft
	if f.State != nil {
		if err := json.Unmarshal(f.State, &d); err != nil {
			return nil, err
		}
	}
	if err := Apply(&d, f.Event); err != nil {
		return nil, err
	}
	return json.Marshal(d)
}

// foldPublished is the records.entityPublished projector's fold: the last
// published version.
func foldPublished(f live.Fold) (json.RawMessage, error) {
	if f.Event.Type != EventPublished {
		return f.State, nil
	}
	p, err := decode[Publication](f.Event.Data)
	if err != nil {
		return nil, err
	}
	p.Entity.ID, p.Entity.Version = f.Event.Target.ID, p.Version
	return json.Marshal(p.Entity)
}
