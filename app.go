package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"time"
)

//go:embed views/*
var templateFS embed.FS

type Middleware func(http.HandlerFunc) http.HandlerFunc

type View struct {
	CurrentUserId int64
	CsrfToken     string
	Home
	NewSite
	Profile
}

type NewSite struct {
	Url  string
	Name string
}

type Home struct {
	Passcode string
	Sites    []Site
}

type Profile struct {
	Email string
}

type App struct {
	model     Model
	logger    Logger
	mux       *http.ServeMux
	templates *template.Template
}

type Logger interface {
	Printf(format string, v ...interface{})
}

func NewApp(logger Logger, model Model) (*App, error) {
	templates := template.Must(template.ParseFS(templateFS, "views/*"))
	app := &App{
		model:     model,
		logger:    logger,
		mux:       http.NewServeMux(),
		templates: templates,
	}
	app.addRoutes()
	return app, nil
}

func (app *App) addRoutes() {
	app.get("/", app.home)
	app.get("/new-signup", app.newSignup)
	app.post("/signup", app.signup)
	app.post("/logout", app.private(app.logout))
	app.get("/new-site", app.private(app.newSite))
	app.post("/create-site", app.private(app.createSite))
	app.get("/profile", app.private(app.profile))
	app.post("/update-profile", app.private(app.updateProfile))

	fileServer := http.FileServer(http.Dir("./static/"))
	app.mux.Handle("/static/", http.StripPrefix("/static", fileServer))
}

func (app *App) newSite(w http.ResponseWriter, r *http.Request) {
	view := View{
		NewSite: NewSite{
			Url:  r.FormValue("url"),
			Name: r.FormValue("name"),
		},
	}
	app.render(w, r, "newSite", view)
}

func (app *App) createSite(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	url := r.FormValue("url")
	userId := app.currentUserId(r) // TODO: context?
	_, err := app.model.CreateSite(userId, name, url)
	if err != nil {
		app.newSite(w, r)
	}
	redirect(w, r, "/")
}

func (app *App) logout(w http.ResponseWriter, r *http.Request) {
	cookie := &http.Cookie{
		Name:     "sesh",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   r.URL.Scheme == "https",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
	http.SetCookie(w, cookie)
	redirect(w, r, "/")
}

func (app *App) home(w http.ResponseWriter, r *http.Request) {
	flash, err := GetFlash(w, r, "passcode")
	haltOn(err)
	view := View{
		Home: Home{
			Passcode: string(flash),
			Sites:    app.model.ListSites(app.currentUserId(r)),
		},
	}
	app.render(w, r, "home", view)
}

func (app *App) newSignup(w http.ResponseWriter, r *http.Request) {
	app.render(w, r, "newSignup", View{})
}

func (app *App) signup(w http.ResponseWriter, r *http.Request) {
	user, err := app.model.CreateUser()
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
	}
	session, err := app.model.CreateSession(user)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
	}
	cookie := &http.Cookie{
		Name:     "sesh",
		Value:    session.SessionId,
		MaxAge:   30 * 24 * 3600,
		Path:     "/",
		Secure:   r.URL.Scheme == "https",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
	http.SetCookie(w, cookie)
	SetFlash(w, "passcode", []byte(user.Passcode))
	_, err = app.model.CreateSite(user.Id, "", r.FormValue("url"))
	redirect(w, r, "/")
}

func (app *App) profile(w http.ResponseWriter, r *http.Request) {
	view := View{
		Profile: Profile{
			Email: app.currentUser(r).Email.String,
		},
	}
	app.render(w, r, "profile", view)
}

func (app *App) updateProfile(w http.ResponseWriter, r *http.Request) {
	err := app.model.UpdateEmail(app.currentUserId(r), r.FormValue("email"))
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
	}
	redirect(w, r, "/profile")
}

func (app *App) private(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if app.currentUserId(r) == 0 {
			location := "/?return-url=" + url.QueryEscape(r.URL.Path)
			redirect(w, r, location)
			return
		}
		h(w, r)
	}
}

func (app *App) sessionId(r *http.Request) string {
	return cookieValue(r, "sesh")
}

func (app *App) currentUser(r *http.Request) *User {
	return app.model.FindCurrentUser(app.sessionId(r))
}

func (app *App) currentUserId(r *http.Request) int64 {
	return app.model.FindCurrentUserId(app.sessionId(r))
}

func (app *App) render(w http.ResponseWriter, r *http.Request, name string, view View) {
	view.CurrentUserId = app.currentUserId(r)
	view.CsrfToken = app.newCsrfToken(w, r)
	err := app.templates.ExecuteTemplate(w, name+".tmpl", view)
	haltOn(err)
}

func (app *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	w.Header().Set("Cache-Control", "no-cache")
	app.mux.ServeHTTP(w, r)
	app.logger.Printf("message=Request finished method=%s path=%s duration=%v", r.Method, r.URL.Path, time.Since(start))
}

func (app *App) get(pattern string, handlerFunc http.HandlerFunc, middleware ...Middleware) {
	app.mux.HandleFunc(pattern, use(allow(handlerFunc, http.MethodGet), middleware...))
}

func (app *App) post(pattern string, handlerFunc http.HandlerFunc, middleware ...Middleware) {
	app.mux.HandleFunc(pattern, use(csrf(allow(handlerFunc, http.MethodPost)), middleware...))
}

// newCSRFToken returns the current session's CSRF token, generating a new one
// and settings the "csrf-token" cookie if not present.
func (app *App) newCsrfToken(w http.ResponseWriter, r *http.Request) string {
	token := generateCSRFToken()
	cookie := &http.Cookie{
		Name:     "csrf-token",
		Value:    token,
		Path:     "/",
		Secure:   r.URL.Scheme == "https",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
	http.SetCookie(w, cookie)
	return token
}

func allow(h http.HandlerFunc, method string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if method != r.Method {
			w.Header().Set("Allow", method)
			http.Error(w, "405 method not allowed", http.StatusMethodNotAllowed)
		} else {
			h(w, r)
		}
	}
}

func use(h http.HandlerFunc, middleware ...Middleware) http.HandlerFunc {
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}

func haltOn(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func redirect(w http.ResponseWriter, r *http.Request, path string) {
	http.Redirect(w, r, path, http.StatusFound)
}

// csrf wraps the given handler ensuring that the CSRF token in the "csrf-token"
// cookie matches the token in the
// "csrf-token" form field.
func csrf(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.FormValue("_csrf")
		cookieValue := cookieValue(r, "csrf-token")
		if token != cookieValue {
			http.Error(w, "invalid CSRF token or cookie", http.StatusBadRequest)
			return
		}
		h(w, r)
	}
}

func cookieValue(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		switch err {
		case http.ErrNoCookie:
			return ""
		default:
			haltOn(err)
		}
	}
	return cookie.Value
}

func generateCSRFToken() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil { // should never fail
		panic(err)
	}
	return hex.EncodeToString(b)
}
