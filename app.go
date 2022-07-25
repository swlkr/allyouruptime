package main

import (
	"crypto/rand"
	"encoding/hex"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Middleware func(http.HandlerFunc) http.HandlerFunc

type View struct {
	SuccessFlash  string
	CurrentUserId int64
	CsrfToken     string
	Home
	NewSite
	Profile
	Login
}

type Login struct {
	Passcode        string
	InvalidPasscode bool
}

type NewSite struct {
	Url      string
	BlankUrl bool
	Name     string
}

type Home struct {
	Passcode string
	Sites    []Site
}

type Profile struct {
	Email    string
	Passcode string
}

type App struct {
	model       Model
	logger      Logger
	mux         *http.ServeMux
	templates   *template.Template
	templateMap map[string]*template.Template
}

type Logger interface {
	Printf(format string, v ...interface{})
}

func templateMap() map[string]*template.Template {
	var templates map[string]*template.Template
	templates = make(map[string]*template.Template)
	files, err := ioutil.ReadDir("./views")
	if err != nil {
		log.Fatal(err)
	}

	for _, f := range files {
		templates[f.Name()] = template.Must(template.ParseFiles("views/layout.tmpl", "views/"+f.Name()))
	}

	return templates
}

func NewApp(logger Logger, model Model) (*App, error) {
	app := &App{
		model:       model,
		logger:      logger,
		mux:         http.NewServeMux(),
		templateMap: templateMap(),
	}
	app.addRoutes()
	return app, nil
}

func (app *App) addRoutes() {
	app.get("/", app.home)
	app.get("/new-signup", app.newSignup)
	app.post("/signup", app.signup)
	app.get("/login", app.login)
	app.post("/sessions", app.createSession)
	app.post("/logout", app.private(app.logout))
	app.get("/new-site", app.private(app.newSite))
	app.post("/create-site", app.private(app.createSite))
	app.get("/profile", app.private(app.profile))
	app.post("/update-profile", app.private(app.updateProfile))
	app.post("/delete-account", app.private(app.deleteAccount))

	fileServer := http.FileServer(http.Dir("./static/"))
	app.mux.Handle("/static/", http.StripPrefix("/static", fileServer))
}

func (app *App) login(w http.ResponseWriter, r *http.Request) {
	view := View{
		Login: Login{
			Passcode:        r.FormValue("passcode"),
			InvalidPasscode: false,
		},
	}
	app.render(w, r, "login", view)
}

func (app *App) createSession(w http.ResponseWriter, r *http.Request) {
	userId := app.model.FindUserFromPasscode(r.FormValue("passcode"))
	if userId == 0 {
		view := View{
			Login: Login{
				Passcode:        r.FormValue("passcode"),
				InvalidPasscode: true,
			},
		}
		app.render(w, r, "login", view)
		return
	}
	session, err := app.model.CreateSession(userId)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
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
	redirect(w, r, "/")
}

func (app *App) newSite(w http.ResponseWriter, r *http.Request) {
	view := View{
		NewSite: NewSite{
			Url:      r.FormValue("url"),
			Name:     r.FormValue("name"),
			BlankUrl: false,
		},
	}
	app.render(w, r, "new-site", view)
}

func (app *App) createSite(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	url := r.FormValue("url")
	userId := app.currentUserId(r) // TODO: context?
	_, err := app.model.CreateSite(userId, name, url)
	if err != nil {
		view := View{
			NewSite: NewSite{
				Url:      r.FormValue("url"),
				Name:     r.FormValue("name"),
				BlankUrl: true,
			},
		}
		app.render(w, r, "new-site", view)
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
	successFlash, err := GetFlash(w, r, "success")
	haltOn(err)
	sites := app.model.ListSites(app.currentUserId(r))
	view := View{
		SuccessFlash: string(successFlash),
		Home: Home{
			Passcode: string(flash),
			Sites:    sites,
		},
	}
	app.render(w, r, "index", view)
}

func (app *App) newSignup(w http.ResponseWriter, r *http.Request) {
	app.render(w, r, "new-signup", View{})
}

func (app *App) signup(w http.ResponseWriter, r *http.Request) {
	user, err := app.model.CreateUser()
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	session, err := app.model.CreateSession(user.Id)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
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
	redirect(w, r, "/")
}

func (app *App) profile(w http.ResponseWriter, r *http.Request) {
	view := View{
		Profile: Profile{
			Email:    app.currentUser(r).Email.String,
			Passcode: app.currentUser(r).Passcode,
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

func (app *App) deleteAccount(w http.ResponseWriter, r *http.Request) {
	err := app.model.DeleteAccount(app.currentUserId(r))
	haltOn(err)
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
	SetFlash(w, "success", []byte("Account deleted successfully"))
	redirect(w, r, "/")
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
	view.CsrfToken = app.setCsrfToken(w, r)
	app.templateMap[name+".tmpl"].ExecuteTemplate(w, "layout.tmpl", view)
}

type ResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *ResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (app *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rw := &ResponseWriter{w, http.StatusOK}
	rw.Header().Set("Cache-Control", "no-cache")
	app.mux.ServeHTTP(rw, r)
	if !strings.HasPrefix(r.URL.Path, "/static/") {
		app.logger.Printf("message=Request finished method=%s path=%s status=%v duration=%v", r.Method, r.URL.Path, rw.statusCode, time.Since(start))
	}
}

func (app *App) get(pattern string, handlerFunc http.HandlerFunc) {
	app.mux.HandleFunc(pattern, notFound(allowGet(handlerFunc), pattern))
}

func (app *App) post(pattern string, handlerFunc http.HandlerFunc) {
	app.mux.HandleFunc(pattern, checkCsrfToken(allowPost(handlerFunc)))
}

func (app *App) setCsrfToken(w http.ResponseWriter, r *http.Request) string {
	if r.Method == http.MethodPost {
		return cookieValue(r, "csrf-token")
	}

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

func allowGet(h http.HandlerFunc) http.HandlerFunc {
	return allow(h, http.MethodGet)
}

func allowPost(h http.HandlerFunc) http.HandlerFunc {
	return allow(h, http.MethodPost)
}

func notFound(h http.HandlerFunc, pattern string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pattern {
			http.Error(w, "404 Not Found", http.StatusNotFound)
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
func checkCsrfToken(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		formToken := r.FormValue("_csrf")
		cookieToken := cookieValue(r, "csrf-token")
		if formToken != cookieToken {
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
