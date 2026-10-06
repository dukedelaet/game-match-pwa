package store

import (
	"context"
	"database/sql"
	"errors"
)

// ListMetros returns metros (id, slug, label only in DTOs).
func (s *Store) ListMetros(ctx context.Context) ([]Metro, error) {
	out := []Metro{}
	err := s.DB.SelectContext(ctx, &out, `SELECT * FROM metros ORDER BY label`)
	return out, err
}

// MetroByID loads a metro.
func (s *Store) MetroByID(ctx context.Context, id string) (*Metro, error) {
	var m Metro
	err := s.DB.GetContext(ctx, &m, `SELECT * FROM metros WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ListGenders returns active genders ordered for display.
func (s *Store) ListGenders(ctx context.Context) ([]Gender, error) {
	out := []Gender{}
	err := s.DB.SelectContext(ctx, &out, `SELECT * FROM genders WHERE active = 1 ORDER BY sort`)
	return out, err
}

// ListTraits returns traits ordered for display.
func (s *Store) ListTraits(ctx context.Context) ([]Trait, error) {
	out := []Trait{}
	err := s.DB.SelectContext(ctx, &out, `SELECT * FROM traits ORDER BY sort`)
	return out, err
}

// RandomPrompt picks a random active prompt for a game kind.
func (s *Store) RandomPrompt(ctx context.Context, gameKind string) (*Prompt, error) {
	var p Prompt
	err := s.DB.GetContext(ctx, &p, `SELECT * FROM prompt_bank WHERE game_kind = ? AND active = 1 ORDER BY RANDOM() LIMIT 1`, gameKind)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// PromptByID loads a prompt.
func (s *Store) PromptByID(ctx context.Context, id string) (*Prompt, error) {
	var p Prompt
	err := s.DB.GetContext(ctx, &p, `SELECT * FROM prompt_bank WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}
