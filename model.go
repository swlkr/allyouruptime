package main

import (
	rnd "crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Session struct {
	Id        int64
	SessionId string
	UserId    sql.NullInt64
	UpdatedAt sql.NullInt64
	CreatedAt int64
}

type User struct {
	Id        int64
	Passcode  string
	Email     sql.NullString
	UpdatedAt sql.NullInt64
	CreatedAt int64
}

type Site struct {
	Id        int64
	UserId    int64
	Name      sql.NullString
	Url       string
	UpdatedAt sql.NullInt64
	CreatedAt int64
}

type Model struct {
	db  *sql.DB
	rnd *rand.Rand
}

func NewModel(db *sql.DB) (Model, error) {
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	model := Model{db, rnd}
	_, err := model.db.Exec(`
		PRAGMA foreign_keys = ON;

		create table if not exists sessions (
			id integer primary key,
			session_id text not null,
			user_id integer not null references users(id) on delete cascade,
			updated_at integer,
			created_at integer not null default(unixepoch())
		);

		create table if not exists users (
			id integer primary key,
			passcode text unique not null,
			email text,
			updated_at integer,
			created_at integer not null default(unixepoch())
		);

		create table if not exists sites (
			id integer primary key,
			user_id integer not null references users(id) on delete cascade,
			name text,
			url text not null constraint url_not_blank check(length(url) > 0),
			updated_at integer,
			created_at integer not null default(unixepoch()),
			unique(user_id, url)
		);

		create table if not exists pings (
			id integer primary key,
			site_id integer not null references sites(id) on delete cascade,
			up integer not null default(0),
			updated_at integer,
			created_at integer not null default(unixepoch())
		);
	`)

	return model, err
}

func (m *Model) CreateUser() (User, error) {
	row := m.db.QueryRow(
		`insert into users (
			passcode
		) values (
			$1
		)
		returning id, passcode, email, updated_at, created_at`,
		m.passcode(),
	)
	user := User{}
	err := row.Scan(&user.Id, &user.Passcode, &user.Email, &user.UpdatedAt, &user.CreatedAt)
	return user, err
}

func (m *Model) CreateSession(userId int64) (Session, error) {
	row := m.db.QueryRow(
		`insert into sessions (
			session_id, user_id
		) values (
			$1, $2
		)
		returning id, session_id, user_id, updated_at, created_at`,
		randomHex(32),
		userId,
	)
	session := Session{}
	err := row.Scan(&session.Id, &session.SessionId, &session.UserId, &session.UpdatedAt, &session.CreatedAt)
	return session, err
}

func (m *Model) DeleteSession(id string) (sql.Result, error) {
	return m.db.Exec(`delete from sessions where id = $1`, id)
}

func nullify(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	} else {
		return sql.NullString{String: s, Valid: true}
	}
}

func (m *Model) CreateSite(userId int64, n string, url string) (Site, error) {
	name := nullify(n)
	row := m.db.QueryRow(
		`
		insert into sites (
			user_id,
			name,
			url
		) values (
			$1, $2, $3
		)
		returning id, user_id, name, url, updated_at, created_at
		`,
		userId, name, url,
	)
	return newSite(row)
}

func (m *Model) FindCurrentUser(sessionId string) *User {
	row := m.db.QueryRow(
		`
		select users.id, users.passcode, users.email, users.updated_at, users.created_at
		from users
		join sessions on sessions.user_id = users.id
		where sessions.session_id = $1
		and sessions.created_at > strftime('%s','now','-90 days')
		order by sessions.created_at desc
		limit 1
		`, sessionId,
	)
	user := User{}
	err := row.Scan(&user.Id, &user.Passcode, &user.Email, &user.UpdatedAt, &user.CreatedAt)
	if err != nil {
		switch err {
		case sql.ErrNoRows:
			return nil
		default:
			haltOn(err)
		}
	}
	return &user
}

func (m *Model) FindCurrentUserId(sessionId string) int64 {
	row := m.db.QueryRow(
		`
		select sessions.user_id
		from sessions
		where session_id = $1
		and created_at > strftime('%s','now','-90 days')
		order by created_at desc
		limit 1
		`, sessionId,
	)
	var id int64
	err := scan(row, &id)
	haltOn(err)
	return id
}

func (m *Model) FindUserFromPasscode(passcode string) int64 {
	row := m.db.QueryRow(
		`
		select id
		from users
		where passcode = $1
		order by created_at desc
		limit 1
		`, passcode,
	)
	var id int64 = 0
	err := row.Scan(&id)
	if err != nil {
		switch err {
		case sql.ErrNoRows:
			return 0
		default:
			haltOn(err)
		}
	}
	return id
}

func scan(row *sql.Row, values ...interface{}) error {
	err := row.Scan(values...)
	if err != nil {
		switch err {
		case sql.ErrNoRows:
			return nil
		default:
			return err
		}
	}
	return nil
}

func (m *Model) ListSites(userId int64) []Site {
	rows, err := m.db.Query(
		`
		select id, user_id, name, url, updated_at, created_at
		from sites
		where user_id = $1
		`, userId,
	)
	haltOn(err)
	defer rows.Close()
	var sites []Site
	for rows.Next() {
		site := Site{}
		err = rows.Scan(&site.Id, &site.UserId, &site.Name, &site.Url, &site.UpdatedAt, &site.CreatedAt)
		haltOn(err)
		sites = append(sites, site)
	}
	if rows.Err() != nil {
		switch rows.Err() {
		case sql.ErrNoRows:
			return nil
		default:
			haltOn(err)
		}
	}
	return sites
}

func (m *Model) UpdateEmail(userId int64, e string) error {
	email := nullify(e)
	_, err := m.db.Exec(
		`
		update users
		set email = $1
		where id = $2
		`,
		email, userId,
	)
	return err
}

func (m *Model) DeleteAccount(userId int64) error {
	_, err := m.db.Exec(`delete from users where id = $1`, userId)
	if err != nil {
		switch err {
		case sql.ErrNoRows:
			return nil
		default:
			return err
		}
	}
	return err
}

func (m *Model) AllSites() []Site {
	rows, err := m.db.Query(
		`select id, user_id, name, url, updated_at, created_at
		from sites
		order by created_at desc`,
	)
	haltOn(err)
	defer rows.Close()
	if rows.Err() != nil {
		switch rows.Err() {
		case sql.ErrNoRows:
			return nil
		default:
			haltOn(err)
		}
	}
	var sites []Site
	for rows.Next() {
		site := Site{}
		err = rows.Scan(&site.Id, &site.UserId, &site.Name, &site.Url, &site.UpdatedAt, &site.CreatedAt)
		haltOn(err)
		sites = append(sites, site)
	}
	return sites
}

func newSite(row *sql.Row) (Site, error) {
	site := Site{}
	err := row.Scan(&site.Id, &site.UserId, &site.Name, &site.Url, &site.UpdatedAt, &site.CreatedAt)
	return site, err
}

func (m *Model) passcode() string {
	max := 9999
	min := 1000

	parts := []string{}
	for i := 0; i < 4; i++ {
		part := fmt.Sprintf("%d", m.rnd.Intn(max-min)+min)
		parts = append(parts, part)
	}

	return strings.Join(parts, " ")
}

func randomHex(n int) string {
	bytes := make([]byte, n)
	_, err := rnd.Read(bytes)
	haltOn(err)
	return hex.EncodeToString(bytes)
}
