package music

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func imageResponse(data []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
		Body:       io.NopCloser(bytes.NewReader(data)),
	}
}

func TestSearchMapsNetEaseResults(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Host != "music.163.com" || request.URL.Path != "/api/cloudsearch/pc" {
			t.Fatalf("request = %s %s", request.Method, request.URL)
		}
		if request.Header.Get("User-Agent") == "" || request.Header.Get("Referer") != "https://music.163.com/" {
			t.Fatalf("headers = %#v", request.Header)
		}
		query := request.URL.Query()
		if query.Get("s") != "晴天" || query.Get("type") != "1" || query.Get("limit") != "10" || query.Get("offset") != "0" {
			t.Fatalf("query = %#v", query)
		}
		return jsonResponse(`{"result":{"songs":[
			{"id":123,"name":"晴天","ar":[{"name":"周杰伦"}],"al":{"name":"叶惠美"},"dt":269000,"no":3,"cd":"01"},
			{"id":"","name":"invalid"}
		]}}`), nil
	})})

	results, err := client.Search(t.Context(), "晴天", "netease", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	result := results[0]
	if result.ID != "123" || result.PictureID != "123" || result.LyricID != "123" {
		t.Fatalf("unexpected identifiers: %#v", result)
	}
	if result.Name != "晴天" || len(result.Artists) != 1 || result.Artists[0] != "周杰伦" || result.Album != "叶惠美" || result.DurationMS != 269000 || result.TrackNumber != 3 || result.DiscNumber != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestSearchMapsNetEaseNumericDisc(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(`{"code":0,"result":{"songs":[{"id":123,"name":"晴天","ar":[],"al":{"name":"叶惠美"},"dt":269000,"no":1,"cd":2}]}}`), nil
	})})
	results, err := client.Search(t.Context(), "晴天", "netease", 10)
	if err != nil || len(results) != 1 || results[0].DiscNumber != 2 {
		t.Fatalf("results = %#v, err = %v", results, err)
	}
}

func TestSearchMapsNetEaseNullableTrackFields(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(`{"code":200,"result":{"songs":[{"id":123,"name":"存在 (Live)","ar":[{"name":"汪峰"}],"al":{"name":"现场"},"dt":278961,"no":null,"cd":null}]}}`), nil
	})})
	results, err := client.Search(t.Context(), "存在 live 汪峰", "netease", 20)
	if err != nil || len(results) != 1 || results[0].TrackNumber != 0 || results[0].DiscNumber != 0 {
		t.Fatalf("results = %#v, err = %v", results, err)
	}
}

func TestClientSourcesAreStable(t *testing.T) {
	client := NewClient(http.DefaultClient)
	sources := client.Sources()
	if len(sources) != 3 || sources[0] != "netease" || sources[1] != "qmusic" || sources[2] != "kuwo" {
		t.Fatalf("sources = %#v", sources)
	}
	sources[0] = "changed"
	if client.Sources()[0] != "netease" {
		t.Fatal("Sources exposed mutable client state")
	}
}

func TestSearchQQMusicMapsResults(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Host != "c.y.qq.com" || request.URL.Path != "/soso/fcgi-bin/client_search_cp" {
			t.Fatalf("request = %s %s", request.Method, request.URL)
		}
		if request.URL.Query().Get("w") != "晴天 周杰伦" || request.URL.Query().Get("n") != "20" {
			t.Fatalf("query = %#v", request.URL.Query())
		}
		body := `{"code":0,"data":{"song":{"list":[{"songmid":"song-mid","songname":"晴天","singer":[{"name":"周杰伦"}],"albummid":"album-mid","albumname":"叶惠美","interval":269,"albumindex":3,"disc":1}]}}}`
		return jsonResponse(body), nil
	})})

	results, err := client.Search(t.Context(), "晴天 周杰伦", "qmusic", 20)
	if err != nil || len(results) != 1 {
		t.Fatalf("results = %#v, error = %v", results, err)
	}
	track := results[0]
	if track.ID != "song-mid" || track.PictureID != "album-mid" || track.LyricID != "song-mid" || track.Source != "qmusic" || track.DurationMS != 269000 || track.TrackNumber != 3 || track.DiscNumber != 1 {
		t.Fatalf("track = %#v", track)
	}
}

func TestQQMusicDetailLoadsCoverAndLyrics(t *testing.T) {
	jpeg := []byte{0xff, 0xd8, 0xff, 0xdb, 0x00, 0x43}
	client := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var response *http.Response
		switch request.URL.Host {
		case "c.y.qq.com":
			response = jsonResponse(`{"code":0,"lyric":"WzAwOjAxLjAwXU9yaWdpbmFs","trans":"WzAwOjAxLjAwXVRyYW5zbGF0ZWQ="}`)
		case "y.qq.com":
			response = imageResponse(jpeg)
		default:
			t.Fatalf("host = %q", request.URL.Host)
		}
		return response, nil
	})})

	assets, err := client.Resolve(t.Context(), "qmusic", "album-mid", "song-mid")
	if err != nil {
		t.Fatal(err)
	}
	if assets.Detail.CoverURL != "https://y.qq.com/music/photo_new/T002R500x500M000album-mid.jpg" || assets.Detail.Lyrics != "[00:01.00]Original" || assets.Detail.TranslatedLyrics != "[00:01.00]Translated" || string(assets.CoverData) != string(jpeg) {
		t.Fatalf("assets = %#v", assets)
	}
}

func TestSearchRetriesSuccessfulEmptyResponse(t *testing.T) {
	var requests atomic.Int32
	client := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "music.163.com" {
			t.Fatalf("host = %q", request.URL.Host)
		}
		if requests.Add(1) == 1 {
			return jsonResponse(`{"code":200,"result":{"songs":[]}}`), nil
		}
		return jsonResponse(`{"code":200,"result":{"songs":[{"id":"1","name":"Song","ar":[{"name":"Artist"}],"al":{"name":"Album"}}]}}`), nil
	})})

	results, err := client.Search(t.Context(), "Song", "netease", 20)
	if err != nil || len(results) != 1 || results[0].Name != "Song" {
		t.Fatalf("results = %#v, error = %v", results, err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d", requests.Load())
	}
}

func TestNetEaseSearchMapsUpstreamFailure(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
	})})

	_, err := client.Search(t.Context(), "Song", "netease", 20)
	if err == nil || err.Error() != "music service request failed" {
		t.Fatalf("error = %v", err)
	}
	if status, ok := err.(interface{ StatusCode() int }); !ok || status.StatusCode() != http.StatusBadGateway {
		t.Fatalf("status error = %#v", err)
	}
}

func TestNetEaseDetailLoadsCoverAndLyrics(t *testing.T) {
	jpeg := []byte{0xff, 0xd8, 0xff, 0xdb, 0x00, 0x43}
	client := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "music.163.com" {
			t.Fatalf("host = %q", request.URL.Host)
		}
		switch request.URL.Path {
		case "/api/song/detail":
			if request.URL.Query().Get("ids") != "[picture-1]" {
				t.Fatalf("query = %#v", request.URL.Query())
			}
			return jsonResponse(`{"songs":[{"id":"picture-1","album":{"picUrl":"https://music.163.com/cover.jpg"}}]}`), nil
		case "/api/song/lyric":
			if request.URL.Query().Get("id") != "lyric-1" || request.URL.Query().Get("lv") != "-1" {
				t.Fatalf("query = %#v", request.URL.Query())
			}
			return jsonResponse(`{"lrc":{"lyric":"[00:01.00]Original"},"tlyric":{"lyric":"[00:01.00]Translated"}}`), nil
		case "/cover.jpg":
			return imageResponse(jpeg), nil
		default:
			t.Fatalf("path = %q", request.URL.Path)
			return nil, nil
		}
	})})

	assets, err := client.Resolve(t.Context(), "netease", "picture-1", "lyric-1")
	if err != nil {
		t.Fatal(err)
	}
	if assets.Detail.CoverURL != "https://music.163.com/cover.jpg" || assets.CoverMIME != "image/jpeg" || string(assets.CoverData) != string(jpeg) {
		t.Fatalf("cover assets = %#v", assets)
	}
	if assets.Detail.Lyrics != "[00:01.00]Original" || assets.Detail.TranslatedLyrics != "[00:01.00]Translated" {
		t.Fatalf("lyrics = %#v", assets.Detail)
	}
}

func TestResolveRejectsNonImageCover(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/api/song/detail" {
			return jsonResponse(`{"songs":[{"id":"picture-1","album":{"picUrl":"https://music.163.com/cover"}}]}`), nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("not an image"))}, nil
	})})

	_, err := client.Resolve(t.Context(), "netease", "picture-1", "")
	if err == nil || err.Error() != "music service returned an invalid cover image" {
		t.Fatalf("error = %v", err)
	}
}

func TestNetEaseSearchResponseCanDecodeNumericAndStringIDs(t *testing.T) {
	var response netEaseSearchEnvelope
	if err := json.Unmarshal([]byte(`{"code":200,"result":{"songs":[{"id":123,"name":"numeric"},{"id":"456","name":"string"}]}}`), &response); err != nil {
		t.Fatal(err)
	}
	if response.Result.Songs[0].ID.String() != "123" || response.Result.Songs[1].ID.String() != "456" {
		t.Fatalf("songs = %#v", response.Result.Songs)
	}
}
