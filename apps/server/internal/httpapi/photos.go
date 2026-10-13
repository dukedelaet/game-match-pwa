package httpapi

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"gamematch/internal/game"
	"gamematch/internal/media"
	"gamematch/internal/store"
)

const maxPhotoBytes = 10 << 20

func (s *Server) uploadPhoto(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	if !s.allow(w, r, "photos:"+u.ID, limitPhotosPerUser, windowPhotos) {
		return
	}
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
		// A photo is visible to its owner and to people they have matched with.
		// "Not blocked" is not enough: photo URLs are handed out in match cards
		// and never revoked, so without this a former match keeps a working link
		// to someone's picture after an unmatch.
		a, b := game.OrderedPair(u.ID, p.UserID)
		m, err := s.Store.MatchFor(ctx, a, b)
		if err != nil || m == nil || m.State != "mutual" {
			notFound(w)
			return
		}
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
	// These are private photos. An explicit no-store keeps them out of shared
	// caches, and nosniff stops a mis-typed file being interpreted as something
	// else by content sniffing.
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, abs)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
