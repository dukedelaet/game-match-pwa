package httpapi

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"gamematch/internal/media"
	"gamematch/internal/store"
)

const maxPhotoBytes = 10 << 20

func (s *Server) uploadPhoto(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxPhotoBytes+1<<20)
	if err := r.ParseMultipartForm(maxPhotoBytes); err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "bad_image", "Could not read that photo")
		return
	}
	file, _, err := r.FormFile("photo")
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "bad_image", "Could not read that photo")
		return
	}
	defer func() { _ = file.Close() }()

	full, thumb, err := media.Process(file, 1080)
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, "bad_image", "Could not read that photo")
		return
	}

	id := uuid.NewString()
	dir := filepath.Join(s.PhotosDir, u.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	relFull := filepath.ToSlash(filepath.Join(u.ID, id+".jpg"))
	relThumb := filepath.ToSlash(filepath.Join(u.ID, id+"_t.jpg"))
	if err := os.WriteFile(filepath.Join(s.PhotosDir, relFull), full, 0o644); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	if err := os.WriteFile(filepath.Join(s.PhotosDir, relThumb), thumb, 0o644); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}

	photo := &store.Photo{ID: id, UserID: u.ID, Path: relFull, ThumbPath: &relThumb, ModerationState: "ok"}
	if err := s.Store.InsertPhoto(ctx, photo); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"photo": PhotoDTO{ID: photo.ID, URL: "/v1/photos/" + photo.ID, State: "ok"},
	})
}

func (s *Server) photo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	p, err := s.Store.PhotoByID(ctx, idParam(r))
	if err != nil {
		notFound(w)
		return
	}
	if p.UserID != u.ID {
		blocked, err := s.Store.BlockedEitherWay(ctx, u.ID, p.UserID)
		if err != nil || blocked {
			notFound(w)
			return
		}
	}
	abs := filepath.Join(s.PhotosDir, filepath.FromSlash(p.Path))
	if !fileExists(abs) {
		notFound(w)
		return
	}
	http.ServeFile(w, r, abs)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
