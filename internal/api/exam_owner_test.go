package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- HTTP contract for exam ownership ---

func TestUploadAcceptsKidID(t *testing.T) {
	e := newPaperTestServer(t)

	// Ownership (kid_id) is specified by the uploading parent as normal business input; the API accepts and persists it.
	status, resp := uploadMultipart(t, e, "/api/exams/upload", "卷子.png",
		pngDoc("attr-ok"), map[string]string{"subject_id": "1", "kid_id": "7"})
	require.Equal(t, http.StatusOK, status)
	require.EqualValues(t, 0, body2int(t, resp["code"]))

	paperID := body2int(t, resp["data"].(map[string]any)["id"])
	code, body := getJSON(t, e, "/api/exams/"+itoa(paperID))
	require.Equal(t, http.StatusOK, code)

	data := body["data"].(map[string]any)
	require.EqualValues(t, 7, data["kid_id"])
	// uploaded_by is fixed server-side to parent (the identity when there is no auth).
	require.EqualValues(t, 1, data["uploaded_by"])
}

func TestUploadRejectsNonNumericKidID(t *testing.T) {
	e := newPaperTestServer(t)

	status, resp := uploadMultipart(t, e, "/api/exams/upload", "卷子.png",
		pngDoc("attr-bad"), map[string]string{"kid_id": "abc"})
	require.Equal(t, http.StatusBadRequest, status)
	require.NotEqualValues(t, 0, body2int(t, resp["code"]))
}

func TestUpdateSwitchesAndClearsKidID(t *testing.T) {
	e := newPaperTestServer(t)
	status, resp := uploadMultipart(t, e, "/api/exams/upload", "卷子.png",
		pngDoc("attr-update"), map[string]string{"subject_id": "1"})
	require.Equal(t, http.StatusOK, status)
	paperID := itoa(body2int(t, resp["data"].(map[string]any)["id"]))

	// Switch ownership to 3.
	code, body := putJSON(t, e, "/api/exams/"+paperID, map[string]any{
		"subject_id": 1, "kid_id": 3,
	})
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 3, body["data"].(map[string]any)["kid_id"])

	// Passing null clears ownership.
	_, body = putJSON(t, e, "/api/exams/"+paperID, map[string]any{
		"subject_id": 1, "kid_id": nil,
	})
	_, hasID := body["data"].(map[string]any)["kid_id"]
	require.False(t, hasID, "取消归属后该字段应因 omitempty 消失")
}

func TestUpdateRejectsNonPositiveKidID(t *testing.T) {
	e := newPaperTestServer(t)
	status, resp := uploadMultipart(t, e, "/api/exams/upload", "卷子.png",
		pngDoc("attr-neg"), map[string]string{})
	require.Equal(t, http.StatusOK, status)
	paperID := itoa(body2int(t, resp["data"].(map[string]any)["id"]))

	code, respBody := putJSON(t, e, "/api/exams/"+paperID, map[string]any{
		"kid_id": 0,
	})
	require.NotEqual(t, http.StatusOK, code, "0 不是有效主键，必须被拒")
	require.NotEqualValues(t, 0, body2int(t, respBody["code"]))
}

func TestListFiltersByKidID(t *testing.T) {
	e := newPaperTestServer(t)

	mk := func(doc, kid string) {
		fields := map[string]string{}
		if kid != "" {
			fields["kid_id"] = kid
		}
		status, resp := uploadMultipart(t, e, "/api/exams/upload", doc+".png", pngDoc(doc), fields)
		require.Equal(t, http.StatusOK, status)
		require.EqualValues(t, 0, body2int(t, resp["code"]))
	}
	mk("own-a", "7")
	mk("own-b", "8")

	code, body := getJSON(t, e, "/api/exams?kid_id=7")
	require.Equal(t, http.StatusOK, code)
	items := body["data"].(map[string]any)["items"].([]any)
	require.Len(t, items, 1, "只返回归属 7 的试卷")
	require.EqualValues(t, 7, items[0].(map[string]any)["kid_id"])
}
