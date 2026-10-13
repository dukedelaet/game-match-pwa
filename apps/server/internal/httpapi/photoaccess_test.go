package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"gamematch/internal/game"
	"gamematch/internal/store"
)

func uploadPhotoID(t *testing.T, c *client) string {
	t.Helper()
	rec := c.upload("/v1/me/photos", "photo", "me.png", pngBytes(t, 2000, 1200))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body map[string]map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body["photo"]["id"]
}

// Photo URLs are handed out in Card.PhotoURL to anyone you have ever been
// paired with, and are never revoked. The serve endpoint only checked "not
// blocked", so any authenticated user holding a URL could fetch that photo
// forever -- including after an unmatch.
func TestUnrelatedUserCannotFetchPhoto(t *testing.T) {
	s, _ := newTestServer(t)

	owner := login(t, s, "+15551111111")
	stranger := login(t, s, "+15553333333")

	photoID := uploadPhotoID(t, owner)

	code, _ := stranger.json(http.MethodGet, "/v1/photos/"+photoID, nil)
	require.Equal(t, http.StatusNotFound, code,
		"an unrelated authenticated user must not be able to read someone's photo")
}

func TestOwnerCanAlwaysFetchOwnPhoto(t *testing.T) {
	s, _ := newTestServer(t)

	owner := login(t, s, "+15551111111")
	photoID := uploadPhotoID(t, owner)

	rec := owner.req(http.MethodGet, "/v1/photos/"+photoID, nil)
	require.Equal(t, http.StatusOK, rec.Code)
}

// Someone you have matched with can see the photo; that is what the URL in a
// match card is for. The fix must not break the match screen.
func TestMatchedUserCanFetchPhoto(t *testing.T) {
	s, st := newTestServer(t)

	alex := login(t, s, "+15551111111")
	jordan := login(t, s, "+15552222222")

	photoID := uploadPhotoID(t, alex)
	connectMatch(t, st, mustUserIDByPhone(t, st, "+15551111111"), mustUserIDByPhone(t, st, "+15552222222"))

	rec := jordan.req(http.MethodGet, "/v1/photos/"+photoID, nil)
	require.Equal(t, http.StatusOK, rec.Code, "a matched user must be able to see the photo")
}

// A former match who unmatched must lose access: that is the case where the
// URL keeps working today.
func TestUnmatchedUserLosesPhotoAccess(t *testing.T) {
	s, st := newTestServer(t)

	alex := login(t, s, "+15551111111")
	jordan := login(t, s, "+15552222222")
	alexID := mustUserIDByPhone(t, st, "+15551111111")
	jordanID := mustUserIDByPhone(t, st, "+15552222222")

	photoID := uploadPhotoID(t, alex)
	connectMatch(t, st, alexID, jordanID)

	require.Equal(t, http.StatusOK, jordan.req(http.MethodGet, "/v1/photos/"+photoID, nil).Code)

	// Alex unmatches.
	code, body := alex.json(http.MethodPost, "/v1/pairs/"+orderedPairID(t, st, alexID, jordanID)+"/unmatch", nil)
	require.Equal(t, http.StatusOK, code, "unmatch body: %v", body)

	rec := jordan.req(http.MethodGet, "/v1/photos/"+photoID, nil)
	require.Equal(t, http.StatusNotFound, rec.Code, "an unmatched user must lose photo access")
}

// connectMatch records a mutual match and its pair relationship, the state two
// people are in after both tap connect.
func connectMatch(t *testing.T, st *store.Store, a, b string) {
	t.Helper()
	ctx := context.Background()
	now := store.NowTS()
	first, second := game.OrderedPair(a, b)
	_, err := st.DB.ExecContext(ctx, `
		INSERT INTO matches (id,user_a,user_b,state,matched_at,created_at,updated_at)
		VALUES (?,?,?,'mutual',?,?,?)`, uuid.NewString(), first, second, now, now, now)
	require.NoError(t, err)
	_, err = st.DB.ExecContext(ctx, `
		INSERT INTO pair_relationships (id,user_a,user_b,state,created_at,updated_at)
		VALUES (?,?,?,'mutual',?,?)`, uuid.NewString(), first, second, now, now)
	require.NoError(t, err)
}

// orderedPairID returns the id of the pair_relationships row for two users.
func orderedPairID(t *testing.T, st *store.Store, a, b string) string {
	t.Helper()
	first, second := game.OrderedPair(a, b)
	var id string
	require.NoError(t, st.DB.GetContext(context.Background(), &id,
		`SELECT id FROM pair_relationships WHERE user_a = ? AND user_b = ?`, first, second))
	return id
}

// End to end: a photo uploaded over HTTP must land on disk owner-readable
// only. The unit tests cover the helpers; this proves the upload path uses them.
func TestUploadedPhotoIsNotWorldReadableOnDisk(t *testing.T) {
	s, _ := newTestServer(t)
	alex := login(t, s, "+15551111111")

	photoID := uploadPhotoID(t, alex)

	var path string
	require.NoError(t, s.Store.DB.GetContext(context.Background(), &path,
		`SELECT path FROM photos WHERE id = ?`, photoID))

	abs := filepath.Join(s.PhotosDir, filepath.FromSlash(path))
	info, err := os.Stat(abs)
	require.NoError(t, err)
	require.Zero(t, info.Mode().Perm()&0o004, "uploaded photo is world-readable: %v", info.Mode().Perm())
	require.Zero(t, info.Mode().Perm()&0o002, "uploaded photo is world-writable: %v", info.Mode().Perm())
}
