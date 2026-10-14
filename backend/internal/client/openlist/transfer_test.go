package openlist

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestDownloadAndOverwriteUpload(t *testing.T) {
	audio := []byte("audio-data")
	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token := r.Header.Get("Authorization"); token != "" {
			t.Fatalf("OpenList token leaked to external download host: %q", token)
		}
		_, _ = w.Write(audio)
	}))
	defer downloadServer.Close()

	var uploaded []byte
	openListServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "user-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/fs/get":
			var request map[string]string
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request["path"] != "/Music/song.mp3" {
				t.Fatalf("path = %q", request["path"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": map[string]any{
					"name":    "song.mp3",
					"size":    len(audio),
					"is_dir":  false,
					"raw_url": downloadServer.URL + "/song.mp3",
				},
			})
		case "/api/fs/put":
			if r.Method != http.MethodPut || r.Header.Get("Overwrite") != "true" {
				t.Fatalf("unexpected upload request: %s overwrite=%q", r.Method, r.Header.Get("Overwrite"))
			}
			path, err := url.PathUnescape(r.Header.Get("File-Path"))
			if err != nil {
				t.Fatal(err)
			}
			if path != "/Music/song.mp3" {
				t.Fatalf("File-Path = %q", path)
			}
			uploaded, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": nil})
		default:
			http.NotFound(w, r)
		}
	}))
	defer openListServer.Close()

	client := NewClient(openListServer.Client())
	var downloaded bytes.Buffer
	var downloadProgress [][2]int64
	file, err := client.DownloadFile(
		t.Context(),
		openListServer.URL,
		"/Music/song.mp3",
		"user-token",
		&downloaded,
		func(transferred, total int64) {
			downloadProgress = append(downloadProgress, [2]int64{transferred, total})
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if file.Name != "song.mp3" || file.Size != int64(len(audio)) || !bytes.Equal(downloaded.Bytes(), audio) {
		t.Fatalf("downloaded file = %#v data=%q", file, downloaded.Bytes())
	}
	if last := downloadProgress[len(downloadProgress)-1]; last != [2]int64{int64(len(audio)), int64(len(audio))} {
		t.Fatalf("final download progress = %v", last)
	}
	tagged := append(downloaded.Bytes(), []byte("-tagged")...)
	var uploadProgress [][2]int64
	if err := client.UploadFile(
		t.Context(),
		openListServer.URL,
		"/Music/song.mp3",
		"user-token",
		bytes.NewReader(tagged),
		int64(len(tagged)),
		"audio/mpeg",
		func(transferred, total int64) {
			uploadProgress = append(uploadProgress, [2]int64{transferred, total})
		},
	); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(uploaded, tagged) {
		t.Fatalf("uploaded = %q", uploaded)
	}
	if last := uploadProgress[len(uploadProgress)-1]; last != [2]int64{int64(len(tagged)), int64(len(tagged))} {
		t.Fatalf("final upload progress = %v", last)
	}
}
