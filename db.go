package main

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io/ioutil"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/kjk/log"
	"github.com/kjk/u"
)

// TODO: use prepared statements where possible

const (
	tagsSepByte                 = 30          // record separator
	snippetSizeThreshold        = 1024        // 1 KB
	cachedContentSizeThresholed = 1024 * 1024 // 1 MB
)

// must match noteinfo.js
const (
	formatText       = "txt"
	formatMarkdown   = "md"
	formatHTML       = "html"
	formatCodePrefix = "code:"
)

// DbUser.ProState
const (
	NotProEligible = iota
	CanBePro
	IsPro
)

var (
	formatNames         = []string{formatText, formatMarkdown, formatHTML, formatCodePrefix}
	sqlDb               *sql.DB
	sqlDbMu             sync.Mutex
	tagSepStr           = string([]byte{30})
	userIDToCachedInfo  map[int]*CachedUserInfo
	contentCache        map[string]*CachedContentInfo
	userIDToDbUserCache map[int]*DbUser

	// general purpose mutex for short-lived ops (like lookup/insert in a map)
	mu sync.Mutex
)

func init() {
	userIDToCachedInfo = make(map[int]*CachedUserInfo)
	contentCache = make(map[string]*CachedContentInfo)
	userIDToDbUserCache = make(map[int]*DbUser)
}

func getSQLConnectionRoot() string {
	if flgIsLocal && !flgProdDb {
		//return "root@tcp(localhost:3306)/"
		return fmt.Sprintf("root@tcp(%s:%s)/", flgDbHost, flgDbPort)
	}
	return "root:7UgJnRvp39vW@tcp(104.196.50.40:3306)/"
}

func getSQLConnection() string {
	return getSQLConnectionRoot() + "quicknotes?parseTime=true"
}

func isValidFormat(s string) bool {
	if strings.HasPrefix(s, formatCodePrefix) {
		return true
	}
	for _, fn := range formatNames {
		if fn == s {
			return true
		}
	}
	return false
}

// CachedContentInfo is content with time when it was cached
type CachedContentInfo struct {
	lastAccessTime time.Time
	d              []byte
}

// DbUser is an information about the user
type DbUser struct {
	ID int
	// TODO: less use of sql.NullString
	Login     string         // e.g. 'google:kkowalczyk@gmail'
	FullName  sql.NullString // e.g. 'Krzysztof Kowalczyk'
	ProState  int
	Email     sql.NullString
	OauthJSON sql.NullString
	CreatedAt time.Time

	handle string // e.g. 'kjk'
}

// GetHandle returns short user handle extracted from login
// "twitter:kjk" => "kjk"
func (u *DbUser) GetHandle() string {
	if len(u.handle) > 0 {
		return u.handle
	}
	parts := strings.SplitN(u.Login, ":", 2)
	if len(parts) != 2 {
		log.Errorf("invalid login '%s'\n", u.Login)
		return ""
	}
	handle := parts[1]
	// if this is an e-mail like kkowalczyk@gmail.com, only return
	// the part before e-mail
	parts = strings.SplitN(handle, "@", 2)
	if len(parts) == 2 {
		handle = parts[0]
	}
	return handle
}

// DbNote describes note in database
type DbNote struct {
	id            int
	userID        int
	CurrVersionID int
	IsDeleted     bool
	IsPublic      bool
	IsStarred     bool
	Size          int
	Title         string
	Format        string
	ContentSha1   []byte
	Tags          []string `json:",omitempty"`
	CreatedAt     time.Time
}

// Note describes note in memory
type Note struct {
	DbNote
	UpdatedAt   time.Time
	Snippet     string
	IsPartial   bool
	IsTruncated bool
	HashID      string
}

// NewNote describes a new note to be inserted into a database
type NewNote struct {
	hashID      string
	title       string
	format      string
	content     []byte
	tags        []string
	createdAt   time.Time
	isDeleted   bool
	isPublic    bool
	contentSha1 []byte
}

func newNoteFromNote(n *Note) (*NewNote, error) {
	var err error
	nn := &NewNote{
		title:       n.Title,
		format:      n.Format,
		tags:        n.Tags,
		createdAt:   n.CreatedAt,
		isDeleted:   n.IsDeleted,
		isPublic:    n.IsPublic,
		contentSha1: n.ContentSha1,
	}
	nn.content, err = getCachedContent(nn.contentSha1)
	return nn, err
}

// CachedUserInfo has cached user info
type CachedUserInfo struct {
	user  *DbUser
	notes []*Note
}

type notesByCreatedAt []*Note

func (s notesByCreatedAt) Len() int {
	return len(s)
}
func (s notesByCreatedAt) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s notesByCreatedAt) Less(i, j int) bool {
	return s[i].CreatedAt.After(s[j].CreatedAt)
}

// SetSnippet sets a short version of note (if is big)
func (n *Note) SetSnippet() {
	var snippetBytes []byte
	// skip if we've already calculated it
	if n.Snippet != "" {
		return
	}

	snippet, err := localStore.GetSnippet(n.ContentSha1)
	if err != nil {
		return
	}
	// TODO: make this trimming when we create snippet sha1
	snippetBytes, n.IsTruncated = getShortSnippet(snippet)
	n.Snippet = strings.TrimSpace(string(snippetBytes))
	//log.Infof("note: %d, snippet size: %d\n", n.Id, len(n.CachedSnippet))
}

// SetCalculatedProperties calculates some props
func (n *Note) SetCalculatedProperties() {
	n.IsPartial = n.Size > snippetSizeThreshold
	n.HashID = hashInt(n.id)
	n.SetSnippet()
}

// Content returns note content
func (n *Note) Content() string {
	content, err := getNoteContent(n)
	if err != nil {
		return ""
	}
	return string(content)
}

func getCachedContent(sha1 []byte) ([]byte, error) {
	k := string(sha1)
	mu.Lock()
	i := contentCache[k]
	if i != nil {
		i.lastAccessTime = time.Now()
	}
	mu.Unlock()
	if i != nil {
		return i.d, nil
	}
	d, err := loadContent(sha1)
	if err != nil {
		return nil, err
	}
	mu.Lock()
	// TODO: cache eviction
	contentCache[k] = &CachedContentInfo{
		lastAccessTime: time.Now(),
		d:              d,
	}
	mu.Unlock()
	return d, nil
}

func getNoteContent(note *Note) ([]byte, error) {
	return getCachedContent(note.ContentSha1)
}

func clearCachedUserInfo(userID int) {
	mu.Lock()
	delete(userIDToCachedInfo, userID)
	mu.Unlock()
}

func getCachedUserInfo(userID int) (*CachedUserInfo, error) {
	mu.Lock()
	i := userIDToCachedInfo[userID]
	mu.Unlock()
	if i != nil {
		log.Verbosef("user '%d', got from cache\n", userID)
		return i, nil
	}
	timeStart := time.Now()
	user, err := dbGetUserByIDCached(userID)
	if user == nil {
		return nil, err
	}
	notes, err := dbGetNotesForUser(user)
	if err != nil {
		return nil, err
	}
	sort.Sort(notesByCreatedAt(notes))
	res := &CachedUserInfo{
		user:  user,
		notes: notes,
	}
	mu.Lock()
	userIDToCachedInfo[userID] = res
	mu.Unlock()
	log.Verbosef("took %s for user '%d'\n", time.Since(timeStart), userID)
	return res, nil
}

func execMust(db *sql.DB, q string, args ...interface{}) {
	log.Verbosef("db.Exec(): %s\n", q)
	_, err := db.Exec(q, args...)
	fatalIfErr(err, fmt.Sprintf("db.Exec('%s')", q))
}

func getCreateDbSQLMust() []byte {
	path := "createdb.sql"
	d := resourcesFromZip[path]
	if len(d) > 0 {
		return d
	}
	d, err := ioutil.ReadFile(path)
	fatalIfErr(err, "getCreateDbSqlMust")
	return d
}

func getCreateDbStatementsMust() []string {
	d := getCreateDbSQLMust()
	return dbSplitMultiStatements(string(d))
}

func dumpCreateDbStatements() {
	a := getCreateDbStatementsMust()
	for _, s := range a {
		fmt.Printf("%s\n\n", s)
	}
}

func createDatabaseMust() {
	log.Verbosef("trying to create the database\n")
	db, err := sql.Open("mysql", getSQLConnectionRoot())
	fatalIfErr(err, "sql.Open()")
	err = db.Ping()
	fatalIfErr(err, "db.Ping()")
	execMust(db, `CREATE DATABASE quicknotes CHARACTER SET utf8 COLLATE utf8_general_ci`)
	db.Close()

	db, err = getQuickNotesDb()
	fatalIfErr(err, "getQuickNotesDb()")
	stmts := getCreateDbStatementsMust()
	for _, stm := range stmts {
		execMust(db, stm)
	}
	db.Close()
	log.Verbosef("created quicknotes database\n")
}

func serializeTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	// in the very unlikely case
	for i, tag := range tags {
		tags[i] = strings.Replace(tag, tagSepStr, "", -1)
	}
	return strings.Join(tags, tagSepStr)
}

func deserializeTags(s string) []string {
	if len(s) == 0 {
		return nil
	}
	return strings.Split(s, tagSepStr)
}

// save to local store and google storage
// we only save snippets locally
func saveContent(d []byte) ([]byte, error) {
	sha1, err := localStore.PutContent(d)
	if err != nil {
		return nil, err
	}
	err = saveNoteToGoogleStorage(sha1, d)
	return sha1, err
}

func loadContent(sha1 []byte) ([]byte, error) {
	d, err := localStore.GetContentBySha1(sha1)
	if err != nil {
		d, err = readNoteFromGoogleStorage(sha1)
		if err != nil {
			return nil, err
		}
		// cache locally
		localStore.PutContent(d)
	}
	return d, nil
}

func dbCreateNewNote(userID int, note *NewNote) (int, error) {
	db := getDbMust()
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if tx != nil {
			tx.Rollback()
		}
	}()

	fatalif(note.contentSha1 == nil, "note.contentSha1 is nil")
	serializedTags := serializeTags(note.tags)

	// for non-imported notes use current time as note creation time
	if note.createdAt.IsZero() {
		note.createdAt = time.Now()
	}
	vals := NewDbVals("notes", 8)
	vals.Add("user_id", userID)
	vals.Add("curr_version_id", 0)
	vals.Add("versions_count", 1)
	vals.Add("created_at", note.createdAt)
	vals.Add("updated_at", note.createdAt)
	vals.Add("content_sha1", note.contentSha1)
	vals.Add("size", len(note.content))
	vals.Add("format", note.format)
	vals.Add("title", note.title)
	vals.Add("tags", serializedTags)
	vals.Add("is_deleted", note.isDeleted)
	vals.Add("is_public", note.isPublic)
	vals.Add("is_starred", false)
	vals.Add("is_encrypted", false)
	res, err := vals.TxInsert(tx)
	if err != nil {
		log.Errorf("tx.Exec('%s') failed with %s\n", vals.Query, err)
		return 0, err
	}

	noteID, err := res.LastInsertId()
	if err != nil {
		log.Errorf("res.LastInsertId() of noteID failed with %s\n", err)
		return 0, err
	}
	vals = NewDbVals("versions", 11)
	vals.Add("note_id", noteID)
	vals.Add("created_at", note.createdAt)
	vals.Add("content_sha1", note.contentSha1)
	vals.Add("size", len(note.content))
	vals.Add("format", note.format)
	vals.Add("title", note.title)
	vals.Add("tags", serializedTags)
	vals.Add("is_deleted", note.isDeleted)
	vals.Add("is_public", note.isPublic)
	vals.Add("is_starred", false)
	vals.Add("is_encrypted", false)
	res, err = vals.TxInsert(tx)
	if err != nil {
		log.Errorf("tx.Exec('%s') failed with %s\n", vals.Query, err)
		return 0, err
	}
	versionID, err := res.LastInsertId()
	if err != nil {
		log.Errorf("res.LastInsertId() of versionId failed with %s\n", err)
		return 0, err
	}
	q := `UPDATE notes SET curr_version_id=? WHERE id=?`
	_, err = tx.Exec(q, versionID, noteID)
	if err != nil {
		log.Errorf("tx.Exec('%s') failed with %s\n", q, err)
		return 0, err
	}
	err = tx.Commit()
	tx = nil
	return int(noteID), err
}

func dbUpdateNoteWith(userID, noteID int, updateFn func(*NewNote)) error {
	note, err := dbGetNoteByID(noteID)
	if err != nil {
		return err
	}
	if userID != note.userID {
		return fmt.Errorf("mismatched note user. noteID: %d, userID: %d, note.userID: %d", noteID, userID, note.userID)
	}
	newNote, err := newNoteFromNote(note)
	if err != nil {
		return err
	}
	updateFn(newNote)
	// TODO: wasteful, because will call dbGetNoteByID() again
	_, err = dbUpdateNote(userID, newNote)
	return err
}

func dbUpdateNoteTitle(userID, noteID int, newTitle string) error {
	return dbUpdateNoteWith(userID, noteID, func(newNote *NewNote) {
		newNote.title = newTitle
	})
}

func dbUpdateNoteTags(userID, noteID int, newTags []string) error {
	return dbUpdateNoteWith(userID, noteID, func(newNote *NewNote) {
		newNote.tags = newTags
	})
}

func needsNewNoteVersion(note *NewNote, existingNote *Note) bool {
	if !bytes.Equal(note.contentSha1, existingNote.ContentSha1) {
		return true
	}
	if note.format != existingNote.Format {
		return true
	}
	if note.title != existingNote.Title {
		return true
	}
	if !strArrEqual(note.tags, existingNote.Tags) {
		return true
	}
	if note.isDeleted != existingNote.IsDeleted {
		return true
	}
	if note.isPublic != existingNote.IsPublic {
		return true
	}
	// TODO: add checks for is_starred
	return false
}

func dbGetSelectCount(query string) (int, error) {
	db := getDbMust()
	n := 0
	err := db.QueryRow(query).Scan(&n)
	return n, err
}

func dbGetUsersCount() (int, error) {
	return dbGetSelectCount(`SELECT count(*) from users`)
}

func dbGetNotesCount() (int, error) {
	return dbGetSelectCount(`SELECT count(*) from notes`)
}

func dbGetVersionsCount() (int, error) {
	return dbGetSelectCount(`SELECT count(*) from versions`)
}

func dbUpdateNote(userID int, note *NewNote) (int, error) {
	noteID, err := dehashInt(note.hashID)
	if err != nil {
		return 0, err
	}
	existingNote, err := dbGetNoteByID(noteID)
	if err != nil {
		return 0, err
	}
	if existingNote.userID != userID {
		return 0, fmt.Errorf("user %d is trying to update note that belongs to user %d", userID, existingNote.userID)
	}

	// don't create new versions if not necessary
	if !needsNewNoteVersion(note, existingNote) {
		return noteID, nil
	}

	db := getDbMust()
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if tx != nil {
			tx.Rollback()
		}
	}()

	if note.createdAt.IsZero() {
		note.createdAt = time.Now()
	}

	noteSize := len(note.content)

	//log.Infof("inserting new version of note %d\n", noteID)
	serializedTags := serializeTags(note.tags)
	vals := NewDbVals("versions", 11)
	vals.Add("note_id", noteID)
	vals.Add("size", noteSize)
	vals.Add("created_at", note.createdAt)
	vals.Add("content_sha1", note.contentSha1)
	vals.Add("format", note.format)
	vals.Add("title", note.title)
	vals.Add("tags", serializedTags)
	vals.Add("is_deleted", note.isDeleted)
	vals.Add("is_public", note.isPublic)
	vals.Add("is_starred", false)
	vals.Add("is_encrypted", false)

	res, err := vals.TxInsert(tx)
	if err != nil {
		log.Errorf("tx.Exec('%s') failed with %s\n", vals.Query, err)
		return 0, err
	}
	versionID, err := res.LastInsertId()
	if err != nil {
		log.Errorf("res.LastInsertId() of versionId failed with %s\n", err)
		return 0, err
	}
	//Maybe: could get versions_count as:
	//q := `SELECT count(*) FROM versions WHERE note_id=?`
	q := `
UPDATE notes SET
  updated_at=?,
  content_sha1=?,
  size=?,
  format=?,
  title=?,
  tags=?,
  is_public=?,
  is_deleted=?,
  curr_version_id=?,
  versions_count = versions_count + 1
WHERE id=?`
	_, err = tx.Exec(q,
		note.createdAt,
		note.contentSha1,
		noteSize,
		note.format,
		note.title,
		serializedTags,
		note.isPublic,
		note.isDeleted,
		versionID,
		noteID)
	if err != nil {
		log.Errorf("tx.Exec('%s') failed with %s\n", q, err)
		return 0, err
	}

	err = tx.Commit()
	tx = nil
	return int(noteID), err
}

// create a new note. if note.createdAt is non-zero value, this is an import
// of from somewhere else, so we want to preserve that
func dbCreateOrUpdateNote(userID int, note *NewNote) (int, error) {
	var err error
	if len(note.content) == 0 {
		return 0, errors.New("empty note content")
	}

	if !isValidFormat(note.format) {
		return 0, fmt.Errorf("invalid format %s", note.format)
	}

	note.contentSha1, err = saveContent(note.content)
	if err != nil {
		log.Errorf("saveContent() failed with %s\n", err)
		return 0, err
	}

	var noteID int
	if note.hashID == "" {
		noteID, err = dbCreateNewNote(userID, note)
		note.hashID = hashInt(noteID)
	} else {
		noteID, err = dbUpdateNote(userID, note)
	}

	clearCachedUserInfo(userID)
	return noteID, err
}

// TODO: also get content_sha1 for each version (requires index on content_sha1
// to be fast) and if this content_sha1 is only referenced by one version,
// delete from google storage
func dbPermanentDeleteNote(userID, noteID int) error {
	defer clearCachedUserInfo(userID)
	db := getDbMust()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if tx != nil {
			tx.Rollback()
		}
	}()
	q := `
DELETE FROM notes
WHERE id=?`
	_, err = db.Exec(q, noteID)
	if err != nil {
		return err
	}
	q = `
DELETE FROM versions
WHERE note_id=?`
	_, err = db.Exec(q, noteID)
	if err != nil {
		return err
	}
	tx = nil
	return nil
}

func dbSetNoteDeleteState(userID, noteID int, isDeleted bool) error {
	db := getDbMust()
	// matching against user_id is not necessary, added just to prevent potential bugs
	q := `UPDATE notes SET is_deleted=? WHERE id=? AND user_id=?`
	_, err := db.Exec(q, isDeleted, noteID, userID)
	if err != nil {
		log.Errorf("db.Exec() failed with '%s'\n", err)
	}
	clearCachedUserInfo(userID)
	return err
}

func dbDeleteNote(userID, noteID int) error {
	return dbSetNoteDeleteState(userID, noteID, true)
}

func dbUndeleteNote(userID, noteID int) error {
	return dbSetNoteDeleteState(userID, noteID, false)
}

func dbSetNotePublicState(userID, noteID int, isPublic bool) error {
	log.Infof("userID: %d, noteID: %d, isPublic: %v\n", userID, noteID, isPublic)
	db := getDbMust()
	// matching against user_id is not necessary, added just to prevent potential bugs
	q := `UPDATE notes SET is_public=? WHERE id=? AND user_id=?`
	_, err := db.Exec(q, isPublic, noteID, userID)
	if err != nil {
		log.Errorf("db.Exec() failed with '%s'\n", err)
	}
	clearCachedUserInfo(userID)
	return err
}

func dbMakeNotePublic(userID, noteID int) error {
	return dbSetNotePublicState(userID, noteID, true)
}

func dbMakeNotePrivate(userID, noteID int) error {
	return dbSetNotePublicState(userID, noteID, false)
}

func dbSetNoteStarredState(userID, noteID int, isStarred bool) error {
	log.Infof("userID: %d, noteID: %d, isStarred: %v\n", userID, noteID, isStarred)
	db := getDbMust()
	// matching against user_id is not necessary, added just to prevent potential bugs
	q := `UPDATE notes SET is_starred=? WHERE id=? AND user_id=?`
	_, err := db.Exec(q, isStarred, noteID, userID)
	if err != nil {
		log.Errorf("db.Exec() failed with '%s'\n", err)
	}
	clearCachedUserInfo(userID)
	return err
}

func dbUnstarNote(userID, noteID int) error {
	return dbSetNoteStarredState(userID, noteID, false)
}

func dbStarNote(userID, noteID int) error {
	return dbSetNoteStarredState(userID, noteID, true)
}

// note: only use locally for testing search, not in production
func dbGetAllNotes() ([]*Note, error) {
	var notes []*Note
	db := getDbMust()
	q := `
SELECT
	id,
	user_id,
	curr_version_id,
	is_deleted,
	is_public,
	is_starred,
	created_at,
	updated_at,
	size,
	format,
	title,
	content_sha1,
	tags
FROM notes
ORDER BY updated_at DESC
LIMIT 10000`
	rows, err := db.Query(q)
	if err != nil {
		log.Errorf("db.Query('%s') failed with %s\n", q, err)
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n Note
		var tagsSerialized string
		err = rows.Scan(
			&n.id,
			&n.userID,
			&n.CurrVersionID,
			&n.IsDeleted,
			&n.IsPublic,
			&n.IsStarred,
			&n.CreatedAt,
			&n.UpdatedAt,
			&n.Size,
			&n.Format,
			&n.Title,
			&n.ContentSha1,
			&tagsSerialized)
		if err != nil {
			return nil, err
		}
		n.Tags = deserializeTags(tagsSerialized)
		n.SetCalculatedProperties()
		notes = append(notes, &n)
	}
	err = rows.Err()
	if err != nil {
		log.Errorf("rows.Err() for '%s' failed with %s\n", q, err)
		return nil, err
	}
	return notes, nil
}

func dbGetAllVersionsSha1ForUser(userID int) ([][]byte, error) {
	db := getDbMust()
	q := `
SELECT content_sha1
FROM versions
WHERE id IN
  (SELECT id FROM notes WHERE user_id = ?);
`
	rows, err := db.Query(q, userID)
	if err != nil {
		log.Errorf("db.Query('%s') failed with %s\n", q, err)
		return nil, err
	}
	var res [][]byte
	defer rows.Close()
	for rows.Next() {
		var sha1 []byte
		err = rows.Scan(&sha1)
		if err != nil {
			log.Errorf("rows.Scan() failed with '%s'\n", err)
			return nil, err
		}
		if len(sha1) != 20 {
			log.Errorf("content_sha1 is %d bytes, should be 20\n", len(sha1))
			return nil, fmt.Errorf("content_sha1 is %d bytes (should be 20)", len(sha1))
		}
		res = append(res, sha1)
	}
	err = rows.Err()
	if err != nil {
		log.Errorf("rows.Err() for '%s' failed with %s\n", q, err)
		return nil, err
	}
	return res, nil
}

func dbGetNotesForUser(user *DbUser) ([]*Note, error) {
	var notes []*Note
	db := getDbMust()
	q := `
SELECT
	id,
	curr_version_id,
	is_deleted,
	is_public,
	is_starred,
	created_at,
	updated_at,
	size,
	format,
	title,
	content_sha1,
	tags
FROM notes
WHERE user_id = ?`
	rows, err := db.Query(q, user.ID)
	if err != nil {
		log.Errorf("db.Query('%s') failed with %s\n", q, err)
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n Note
		var tagsSerialized string
		err = rows.Scan(
			&n.id,
			&n.CurrVersionID,
			&n.IsDeleted,
			&n.IsPublic,
			&n.IsStarred,
			&n.CreatedAt,
			&n.UpdatedAt,
			&n.Size,
			&n.Format,
			&n.Title,
			&n.ContentSha1,
			&tagsSerialized)
		if err != nil {
			return nil, err
		}
		n.userID = user.ID
		n.Tags = deserializeTags(tagsSerialized)
		n.SetCalculatedProperties()
		notes = append(notes, &n)
	}
	err = rows.Err()
	if err != nil {
		log.Errorf("rows.Err() for '%s' failed with %s\n", q, err)
		return nil, err
	}
	return notes, nil
}

var (
	recentPublicNotesCached     []Note
	recentPublicNotesLastUpdate time.Time
)

func timeExpired(t time.Time, dur time.Duration) bool {
	return t.IsZero() || time.Now().Sub(t) > dur
}

func getRecentPublicNotesCached(limit int) ([]Note, error) {
	var res []Note

	mu.Lock()
	defer mu.Unlock()
	needsRefreshFromDB := limit > len(recentPublicNotesCached) || timeExpired(recentPublicNotesLastUpdate, time.Minute*5)
	if !needsRefreshFromDB {
		res = make([]Note, limit, limit)
		for i := 0; i < limit; i++ {
			res[i] = recentPublicNotesCached[i]
		}
	}
	if len(res) == limit {
		return res, nil
	}

	db := getDbMust()
	q := `
SELECT
  id,
  user_id,
	curr_version_id,
	is_deleted,
	is_public,
	is_starred,
	created_at,
	updated_at,
	size,
	format,
	title,
	content_sha1,
	tags
FROM notes
WHERE is_public=true
ORDER BY updated_at DESC
LIMIT %d`
	rows, err := db.Query(fmt.Sprintf(q, limit))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n Note
		var tagsSerialized string
		err = rows.Scan(
			&n.id,
			&n.userID,
			&n.CurrVersionID,
			&n.IsDeleted,
			&n.IsPublic,
			&n.IsStarred,
			&n.CreatedAt,
			&n.UpdatedAt,
			&n.Size,
			&n.Format,
			&n.Title,
			&n.ContentSha1,
			&tagsSerialized)
		if err != nil {
			return nil, err
		}
		n.Tags = deserializeTags(tagsSerialized)
		n.SetCalculatedProperties()
		res = append(res, n)
	}
	err = rows.Err()
	if err != nil {
		log.Errorf("rows.Err() for '%s' failed with %s\n", q, err)
		return nil, err
	}

	n := len(res)
	recentPublicNotesCached = make([]Note, n, n)
	for i := 0; i < n; i++ {
		recentPublicNotesCached[i] = res[i]
	}
	recentPublicNotesLastUpdate = time.Now()
	return res, nil
}

func trimTitle(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return nonWhitespaceRightTrim(s[:maxLen])
}

func getTitleFromBody(note *Note) string {
	content, err := getNoteContent(note)
	if err != nil {
		return ""
	}
	return string(getFirstLine(content))
}

func dbGetNoteByID(id int) (*Note, error) {
	var n Note
	var tagsSerialized string
	db := getDbMust()
	q := `
SELECT
  id,
  user_id,
  curr_version_id,
  is_deleted,
  is_public,
  is_starred,
  created_at,
  updated_at,
  size,
  format,
  title,
  content_sha1,
  tags
FROM notes
WHERE id=?`
	err := db.QueryRow(q, id).Scan(
		&n.id,
		&n.userID,
		&n.CurrVersionID,
		&n.IsDeleted,
		&n.IsPublic,
		&n.IsStarred,
		&n.CreatedAt,
		&n.UpdatedAt,
		&n.Size,
		&n.Format,
		&n.Title,
		&n.ContentSha1,
		&tagsSerialized)
	if err != nil {
		return nil, err
	}
	n.Tags = deserializeTags(tagsSerialized)
	n.SetCalculatedProperties()
	return &n, nil
}

func isValidProState(proState int) bool {
	switch proState {
	case NotProEligible, CanBePro, IsPro:
		return true
	default:
		return false
	}
}

// id, login, full_name, email, created_at, pro_state
func dbGetUserByQuery(q string, args ...interface{}) (*DbUser, error) {
	var user DbUser
	db := getDbMust()
	err := db.QueryRow(q, args...).Scan(&user.ID, &user.Login, &user.FullName, &user.Email, &user.CreatedAt, &user.ProState)
	if err != nil {
		if err != sql.ErrNoRows {
			log.Errorf("db.QueryRow('%s', %v) failed with '%s'\n", q, args, err)
			return nil, err
		}
		return nil, nil
	}
	if !isValidProState(user.ProState) {
		return nil, fmt.Errorf("invalid ProState '%d' for user from query '%s'", user.ProState, q)
	}
	return &user, nil
}

func dbGetUserByIDCached(userID int) (*DbUser, error) {
	var res *DbUser
	mu.Lock()
	res = userIDToDbUserCache[userID]
	mu.Unlock()
	if res != nil {
		return res, nil
	}
	res, err := dbGetUserByID(userID)
	if err != nil {
		return nil, err
	}
	mu.Lock()
	userIDToDbUserCache[userID] = res
	mu.Unlock()
	return res, nil
}

func dbGetUserByID(userID int) (*DbUser, error) {
	q := `SELECT id, login, full_name, email, created_at, pro_state FROM users WHERE id=?`
	return dbGetUserByQuery(q, userID)
}

func dbGetUserByLogin(login string) (*DbUser, error) {
	q := `SELECT id, login, full_name, email, created_at, pro_state FROM users WHERE login=?`
	return dbGetUserByQuery(q, login)
}

func dbGetAllUsers() ([]*DbUser, error) {
	db := getDbMust()
	q := `SELECT id, login, full_name, email, created_at, pro_state FROM users`
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []*DbUser
	for rows.Next() {
		var user DbUser
		err = rows.Scan(&user.ID, &user.Login, &user.FullName, &user.Email, &user.CreatedAt)
		if err != nil {
			return nil, err
		}
		res = append(res, &user)
	}
	return res, rows.Err()
}

// TODO: also insert oauthJSON
func dbGetOrCreateUser(userLogin string, fullName string) (*DbUser, error) {
	user, err := dbGetUserByLogin(userLogin)
	if user != nil {
		u.PanicIfErr(err)
		return user, nil
	}

	db := getDbMust()
	vals := NewDbVals("users", 3)
	vals.Add("login", userLogin)
	vals.Add("full_name", fullName)
	vals.Add("pro_state", NotProEligible)
	_, err = vals.Insert(db)
	if err != nil {
		return nil, err
	}
	// TODO: insert default notes
	return dbGetUserByLogin(userLogin)
}

func getQuickNotesDb() (*sql.DB, error) {
	db, err := sql.Open("mysql", getSQLConnection())
	if err != nil {
		return nil, err
	}
	err = db.Ping()
	if err == nil {
		return db, nil
	}
	db.Close()
	return db, err
}

// note: no locking. the presumption is that this is called at startup and
// available throughout the lifetime of the program
func getDbMust() *sql.DB {
	sqlDbMu.Lock()
	defer sqlDbMu.Unlock()

	if sqlDb != nil {
		return sqlDb
	}

	db, err := getQuickNotesDb()
	if err != nil {
		if strings.Contains(err.Error(), "Error 1049") {
			log.Verbosef("db.Ping() failed because no database exists\n")
			createDatabaseMust()
		} else {
			fatalIfErr(err, "getQuickNotesDb")
		}
	}

	db, err = getQuickNotesDb()
	fatalIfErr(err, "getQuickNotesDb")
	err = upgradeDb(db)
	if err != nil {
		log.Fatalf("upgradeDb() failed with '%s'\n", err)
	}
	sqlDb = db
	return sqlDb
}

func closeDb() {
	if sqlDb != nil {
		sqlDb.Close()
		sqlDb = nil
	}
}
