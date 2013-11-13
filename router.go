package main

import (
	"github.com/nickstenning/router/triemux"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// Router is a wrapper around an HTTP multiplexer (trie.Mux) which retrieves its
// routes from a passed mongo database.
type Router struct {
	mux  *triemux.Mux
	stor Storage
}

type Application struct {
	ApplicationId string `bson:"application_id"`
	BackendURL    string `bson:"backend_url"`
}

type Route struct {
	IncomingPath  string `bson:"incoming_path"`
	ApplicationId string `bson:"application_id"`
	RouteType     string `bson:"route_type"`
}

// NewRouter returns a new empty router instance. You will still need to call
// ReloadRoutes() to do the initial route load.
func NewRouter(stor Storage) *Router {
	return &Router{
		mux:  triemux.NewMux(),
		stor: stor,
	}
}

// ServeHTTP delegates responsibility for serving requests to the proxy mux
// instance for this router.
func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt.mux.ServeHTTP(w, r)
}

// ReloadRoutes reloads the routes for this Router instance on the fly. It will
// create a new proxy mux, load applications (backends) and routes into it, and
// then flip the "mux" pointer in the Router.
func (rt *Router) ReloadRoutes() {
	// save a reference to the previous mux in case we have to restore it
	oldmux := rt.mux
	defer func() {
		if r := recover(); r != nil {
			log.Println("router: recovered from panic in ReloadRoutes:", r)
			rt.mux = oldmux
			log.Println("router: original routes have been restored")
		}
	}()

	log.Println("connecting to storage")
	err := rt.stor.Open()
	if err != nil {
		panic(err)
	}
	defer rt.stor.Close()

	log.Printf("router: reloading routes")
	newmux := triemux.NewMux()

	apps := loadApplications(rt.stor)
	loadRoutes(rt.stor, newmux, apps)

	rt.mux = newmux
	log.Printf("router: reloaded routes")
}

// loadApplications is a helper function which loads applications from the
// passed mongo collection and registers them as backends with the passed proxy
// mux.
func loadApplications(stor Storage) (apps map[string]http.Handler) {
	app := &Application{}
	apps = make(map[string]http.Handler)

	iter, err := stor.Applications()

	if err != nil {
		panic(err)
	}

	for iter.Next(&app) {
		backendUrl, err := url.Parse(app.BackendURL)
		if err != nil {
			log.Printf("router: couldn't parse URL %s for backend %s "+
				"(error: %v), skipping!", app.BackendURL, app.ApplicationId, err)
			continue
		}

		proxy := httputil.NewSingleHostReverseProxy(backendUrl)
		// Allow the proxy to keep more than the default (2) keepalive connections
		// per upstream.
		proxy.Transport = &http.Transport{MaxIdleConnsPerHost: 20}

		apps[app.ApplicationId] = proxy
	}

	if err := iter.Err(); err != nil {
		panic(err)
	}

	return
}

// loadRoutes is a helper function which loads routes from the passed mongo
// collection and registers them with the passed proxy mux.
func loadRoutes(stor Storage, mux *triemux.Mux, apps map[string]http.Handler) {
	route := &Route{}

	iter, err := stor.Routes()

	if err != nil {
		panic(err)
	}

	for iter.Next(&route) {
		handler, ok := apps[route.ApplicationId]
		if !ok {
			log.Printf("router: found route %+v which references unknown application "+
				"%s, skipping!", route, route.ApplicationId)
			continue
		}

		prefix := (route.RouteType == "prefix")
		mux.Handle(route.IncomingPath, prefix, handler)
		log.Printf("router: registered %s (prefix: %v) for %s",
			route.IncomingPath, prefix, route.ApplicationId)
	}

	if err := iter.Err(); err != nil {
		panic(err)
	}
}
