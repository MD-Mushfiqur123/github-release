package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"

	"github.com/kevinburke/rest/restclient"
	"github.com/tomnomnom/linkheader"
)

const DefaultBaseURL = "https://api.github.com"

// Set to values > 0 to control verbosity, for debugging.
var VERBOSITY = 0

// DoAuthRequest ...
//
// TODO: This function is amazingly ugly (separate headers, token, no API
// URL constructions, et cetera).
func DoAuthRequest(method, url, mime, token string, headers map[string]string, body io.Reader) (*http.Response, error) {
	req, err := newAuthRequest(method, url, mime, token, headers, body)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Client collects a few options that can be set when contacting the GitHub
// API, such as authorization tokens. Methods called on Client will supply
// these options when calling the API.
type Client struct {
	client *restclient.Client
}

// NewClient creates a new Client for use with the Github API.
func NewClient(username, token string, client *restclient.Client) Client {
	c := Client{}
	if client == nil {
		c.client = restclient.New(username, token, DefaultBaseURL)
	} else {
		c.client = client
	}
	return c
}

// SetBaseURL updates the client's base URL, if baseurl is a non-empty value.
func (c Client) SetBaseURL(baseurl string) {
	// This is lazy, because the caller always tries to override the base URL
	// with EnvApiEndpoint and we used to ignore that at the top of Get, but we
	// don't do that now. So instead just filter out/ignore the empty string.
	if baseurl == "" {
		return
	}
	c.client.Base = baseurl
}

// Get fetches uri (relative URL) from the GitHub API and unmarshals the
// response into v. It takes care of pagination transparantly.
func (c Client) Get(uri string, v any) error {
	rc, err := c.getPaginated(uri)
	if err != nil {
		return err
	}
	defer rc.Close()
	var r io.Reader = rc
	if VERBOSITY > 0 {
		vprintln("BODY:")
		r = io.TeeReader(rc, os.Stderr)
	}

	// Github may return paginated responses. If so, githubGetPaginated will
	// return a reader which yields the concatenation of all pages. These
	// reponses are _separate_ JSON arrays. Standard json.Unmarshal() or
	// json.Decoder.Decode() will not have the expected result when
	// unmarshalling into v. For example, a 2-page response:
	//
	//   1. [{...}, {...}, {...}]
	//   2. [{...}]
	//
	// If v is a slice type, we'd like to decode the four objects from the
	// two pages into a single slice. However, if we just use
	// json.Decoder.Decode(), that won't work. v will be overridden each
	// time.
	//
	// For this reason, we use two very ugly things.
	//
	//   1. We analyze v with reflect to see if it's a slice.
	//   2. If so, we use the json.Decoder token API and reflection to
	//      dynamically add new elements into the slice, ignoring the
	//      boundaries between JSON arrays.
	//
	// This is a lot of work, and feels very stupid. An alternative would be
	// removing the outermost ][ in the intermediate responses, which would
	// be even more finnicky. Another alternative would be to explicitly
	// expose a pagination API, forcing clients of this code to deal with
	// it. That's how the go-github library does it. But why solve a problem
	// sensibly if one can power through it with reflection (half-joking)?

	sl := reflect.Indirect(reflect.ValueOf(v)) // Get the reflect.Value of the slice so we can append to it.
	t := sl.Type()
	if t.Kind() != reflect.Slice {
		// Not a slice, not going to handle special pagination JSON stream
		// semantics since it likely wouldn't work properly anyway. If this
		// is a non-paginated stream, it should work.
		return json.NewDecoder(r).Decode(v)
	}
	t = t.Elem() // Extract the type of the slice's elements.

	// Use streaming Token API to append all elements of the JSON stream
	// arrays (pagination) to the slice.
	for dec := json.NewDecoder(r); ; {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return nil // Natural end of the JSON stream.
			}
			return err
		}
		vprintf("TOKEN %T: %v
", tok, tok)
		// Check for tokens until we get an opening array brace. If we're
		// not in an array, we can't decode an array element later, which
		// would result in an error.
		if tok != json.Delim('[') {
			continue
		}

		// Read the array, appending all elements to the slice.
		for dec.More() {
			it := reflect.New(t) // Interface to a valid pointer to an object of the same type as the slice elements.
			if err := dec.Decode(it.Interface()); err != nil {
				return err
			}
			vprintf("OBJECT %T: %v
", it.Interface(), it)
			sl.Set(reflect.Append(sl, it.Elem()))
		}
	}
}
