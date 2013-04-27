// Package proxymux implements a reverse proxy multiplexer which can be used to
// serve responses from multiple distinct backend servers within a single URL
// hierarchy.
package proxymux

import (
	"github.com/nickstenning/trie"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
)

type Mux struct {
	mu            sync.RWMutex
	nextBackendId int
	backends      map[int]*httputil.ReverseProxy
	trie          *trie.Trie
}

type muxEntry struct {
	prefix    bool
	backendId int
}

// NewMux makes a new empty Mux.
func NewMux() *Mux {
	return &Mux{
		trie:     trie.NewTrie(),
		backends: make(map[int]*httputil.ReverseProxy),
	}
}

// ServeHTTP dispatches the request to a backend with a registered route
// matching the request path, or 404s.
func (mux *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, ok := mux.Lookup(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	proxy, ok := mux.GetBackend(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	proxy.ServeHTTP(w, r)
}

// AddBackend adds a backend for the provided target url, and returns the
// generated backend id. The url should have a scheme, host, and optionally a
// port and path. If the url has a path, then requests will be rewritten onto
// that path, i.e. if a request is for
//
//   /bar
//
// and the matching backend has a target url of
//
//   /foo
//
// then the resulting request will be for
//
//   /foo/bar
func (mux *Mux) AddBackend(target *url.URL) (backendId int) {
	mux.mu.Lock()
	defer mux.mu.Unlock()

	backendId = mux.nextBackendId

	proxy := httputil.NewSingleHostReverseProxy(target)
	// Allow the proxy to keep more than the default (2) keepalive connections per
	// upstream.
	proxy.Transport = &http.Transport{MaxIdleConnsPerHost: 20}

	mux.backends[backendId] = proxy
	mux.nextBackendId++
	return
}

// GetBackend retrieves the registered backend with the given id.
func (mux *Mux) GetBackend(backendId int) (proxy *httputil.ReverseProxy, ok bool) {
	mux.mu.RLock()
	defer mux.mu.RUnlock()

	b, ok := mux.backends[backendId]
	if !ok {
		return nil, false
	}

	return b, true
}

// Lookup takes a path and looks up its registered entry in the mux trie,
// returning the id of the matching backend, if any.
func (mux *Mux) Lookup(path string) (backendId int, ok bool) {
	mux.mu.RLock()
	defer mux.mu.RUnlock()

	entry, ok := findlongestmatch(mux.trie, path)
	return entry.backendId, ok
}

// Register registers the specified route (either an exact or a prefix route)
// and associates it with the specified backend. Requests through the mux for
// paths matching the route will be proxied to that backend.
func (mux *Mux) Register(path string, prefix bool, backendId int) {
	mux.mu.Lock()
	defer mux.mu.Unlock()

	mux.trie.Set(splitpath(path), muxEntry{
		prefix:    prefix,
		backendId: backendId,
	})
}

// splitpath turns a slash-delimited string into a lookup path (a slice
// containing the strings between slashes). Any leading slashes are stripped
// before the string is split.
func splitpath(path string) []string {
	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}
	if path == "" {
		return []string{}
	}
	return strings.Split(path, "/")
}

// findlongestmatch will search the passed trie for the longest route matching
// the passed path, taking into account whether or not each muxEntry is a prefix
// route.
//
// The function first attempts an exact match, and if it fails to find one will
// then chop slash-delimited sections off the end of the path in an attempt to
// find a matching exact or prefix route.
func findlongestmatch(t *trie.Trie, path string) (entry muxEntry, ok bool) {
	origpath := splitpath(path)
	copypath := origpath

	// This search algorithm is potentially abusable -- it will take a
	// (relatively) long time to establish that a path with an enormous number of
	// slashes in doesn't have a corresponding route. The obvious fix is for the
	// trie to keep track of how long its longest root-to-leaf path is and
	// shortcut the lookup by chopping the appropriate number of elements off the
	// end of the lookup.
	//
	// Worrying about the above is probably premature optimization, so I leave the
	// mitigation described as an exercise for the reader.
	for len(copypath) >= 0 {
		val, ok := t.Get(copypath)
		if !ok {
			if len(copypath) > 0 {
				copypath = copypath[:len(copypath)-1]
				continue
			}
			break
		}

		ent, ok := val.(muxEntry)
		if !ok {
			log.Printf("findlongestmatch: got value (%v) from trie that wasn't a muxEntry!", val)
			break
		}

		if len(copypath) == len(origpath) {
			return ent, true
		}

		if ent.prefix {
			return ent, true
		}

		if len(copypath) > 0 {
			copypath = copypath[:len(copypath)-1]
			continue
		}

		// Fell through without finding anything or explicitly calling continue, so:
		break
	}
	return muxEntry{}, false
}