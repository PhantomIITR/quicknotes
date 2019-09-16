package main

import (
	"context"
	"io/ioutil"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/kjk/quicknotes/pkg/log"
	"github.com/kjk/u"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TODO: use prepared statements where possible

const (
	tagsSepByte                 = 30          // record separator
	snippetSizeThreshold        = 1024        // 1 KB
	cachedContentSizeThresholed = 1024 * 1024 // 1 MB
)

// must match Note.js
const (
	formatText       = "txt"
	formatMarkdown   = "md"
	formatHTML       = "html"
	formatCodePrefix = "code:"
)

var (
	formatNames         = []string{formatText, formatMarkdown, formatHTML, formatCodePrefix}
	tagSepStr           = string([]byte{30})
	userIDToCachedInfo  map[string]*CachedUserInfo
	contentCache        map[string]*CachedContentInfo
	userIDToDbUserCache map[string]*DbUser

	// general purpose mutex for short-lived ops (like lookup/insert in a map)
	mu sync.Mutex
)

func init() {
	userIDToCachedInfo = make(map[string]*CachedUserInfo)
	contentCache = make(map[string]*CachedContentInfo)
	userIDToDbUserCache = make(map[string]*DbUser)
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
	ID string
	// e.g. 'google:kkowalczyk@gmail'
	Login string `firestore:"login"`
	// e.g. 'Krzysztof Kowalczyk'
	FullName  string    `firestore:"full_name"`
	Email     string    `firestore:"email"`
	OauthJSON string    `firestore:"oauthjson"`
	CreatedAt time.Time `firestore:"created_at"`

	// e.g. 'kjk'
	handle string
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
	id     string
	userID string

	CurrVersionID   string    `firestore:"curr_version_id"`
	IsDeleted       bool      `firestore:"is_deleted"`
	IsPublic        bool      `firestore:"is_public"`
	IsStarred       bool      `firestore:"is_starred"`
	IsLatestVersion bool      `firestore:"is_latest_ver"`
	Size            int       `firestore:"size"`
	Title           string    `firestore:"title"`
	Format          string    `firestore:"format"`
	Content         string    `firextore:"content"`
	Tags            []string  `firestore:"tags"`
	CreatedAt       time.Time `firestore:"created_at"`
	UpdatedAt       time.Time `firestore:"updated_at"`
}

// Note describes note in memory
type Note struct {
	*DbNote

	Snippet     string
	IsPartial   bool
	IsTruncated bool
}

// NewNote describes a new note to be inserted into a database
type NewNote struct {
	id          string
	title       string
	format      string
	content     []byte
	tags        []string
	createdAt   time.Time
	updatedAt   time.Time
	isDeleted   bool
	isPublic    bool
	isStarred   bool
	contentSha1 []byte
}

func newNoteFromNote(n *Note) (*NewNote, error) {
	var err error
	nn := &NewNote{
		id:        n.id,
		title:     n.Title,
		format:    n.Format,
		tags:      n.Tags,
		createdAt: n.CreatedAt,
		updatedAt: n.UpdatedAt,
		isDeleted: n.IsDeleted,
		isPublic:  n.IsPublic,
		isStarred: n.IsStarred,
		//contentSha1: n.ContentSha1, // TODO(port)
	}
	nn.content, err = getCachedContent(nn.contentSha1)
	return nn, err
}

// CachedUserInfo has cached user info
type CachedUserInfo struct {
	user          *DbUser
	notes         []*Note
	latestVersion string
}

// SetSnippet sets a short version of note (if is big)
func (n *Note) SetSnippet() {
	var snippetBytes []byte
	// skip if we've already calculated it
	if n.Snippet != "" {
		return
	}

	panic("NYI")
	/*
		snippet, err := localStore.GetSnippet(n.ContentSha1)
		if err != nil {
			return
		}
		// TODO: make this trimming when we create snippet sha1
		snippetBytes, n.IsTruncated = getShortSnippet(snippet)
		n.Snippet = strings.TrimSpace(string(snippetBytes))
	*/
	//log.Verbosef("note: %d, snippet size: %d\n", n.Id, len(n.CachedSnippet))
}

// SetCalculatedProperties calculates some props
func (n *Note) SetCalculatedProperties() {
	n.IsPartial = n.Size > snippetSizeThreshold
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
	d, err := localStore.GetContentBySha1(sha1)
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
	return []byte(note.Content()), nil
}

func clearCachedUserInfo(userID string) {
	mu.Lock()
	delete(userIDToCachedInfo, userID)
	mu.Unlock()
}

// TODO: a probably more robust way would be
// q := `select id from versions where user_id=$1 order by id desc limit 1;`
// but we would need user_id on versions table
func findLatestVersion(notes []*Note) int {
	id := 0
	/*
		for _, note := range notes {
			if note.CurrVersionID > id {
				id = note.CurrVersionID
			}
		}
	*/
	return id
}

func getCachedUserInfo(userID string) (*CachedUserInfo, error) {
	mu.Lock()
	i := userIDToCachedInfo[userID]
	mu.Unlock()

	if i != nil {
		return i, nil
	}
	timeStart := time.Now()
	user, err := dbGetUserByIDCached(userID)
	if user == nil || err != nil {
		return nil, err
	}
	notes, err := dbGetNotesForUser(user)
	if err != nil {
		return nil, err
	}
	sort.Slice(notes, func(i, j int) bool {
		return notes[i].CreatedAt.After(notes[j].CreatedAt)
	})
	res := &CachedUserInfo{
		user:  user,
		notes: notes,
		//latestVersion: findLatestVersion(notes),
	}

	mu.Lock()
	userIDToCachedInfo[userID] = res
	mu.Unlock()
	log.Verbosef("took %s for user '%d'\n", time.Since(timeStart), userID)
	return res, nil
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

const (
	collectionUsers = "users"
	collectionNotes = "notes"
)

func dbCreateNewNote(userID string, note *NewNote) (string, error) {
	log.Verbosef("creating a new note '%s' for user %d\n", note.title, userID)

	u.PanicIf(note.contentSha1 == nil, "note.contentSha1 is nil")
	serializedTags := serializeTags(note.tags)

	noteID := genUniqueID()
	client := getFirestoreClientMust()
	docRef := client.Collection(collectionUsers).Doc(userID)
	docRef = docRef.Collection(collectionNotes).Doc(noteID)

	// for non-imported notes use current time as note creation time
	if note.createdAt.IsZero() {
		note.createdAt = time.Now()
	}

	ctx := context.Background()
	dbNote := &DbNote{}
	dbNote.Format = note.format
	dbNote.userID = userID
	/*
		vals := NewDbVals("notes", 8)
		vals.Add("curr_version_id", 0)
		vals.Add("versions_count", 1)
		vals.Add("created_at", note.createdAt)
		vals.Add("updated_at", note.createdAt)
		vals.Add("content_sha1", note.contentSha1)
		vals.Add("size", len(note.content))
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

		docRef.Set(ctx, note)

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
	*/
	panic("NYI")
	return "", nil
}

// most operations mark a note as updated except for starring, which is why
// we need markUpdated
func dbUpdateNote2(note *NewNote, markUpdated bool) (string, error) {
	log.Verbosef("noteID: %d, markUpdated: %v\n", note.id, markUpdated)
	panic("NYI")
	/*
			now := time.Now()
			if note.createdAt.IsZero() {
				note.createdAt = now
				log.Verbosef("note.createdAt is zero, setting to %s\n", note.createdAt)
			}

			noteSize := len(note.content)

			serializedTags := serializeTags(note.tags)
			vals := NewDbVals("versions", 11)
			vals.Add("note_id", note.id)
			vals.Add("size", noteSize)
			vals.Add("created_at", now)
			vals.Add("content_sha1", note.contentSha1)
			vals.Add("format", note.format)
			vals.Add("title", note.title)
			vals.Add("tags", serializedTags)
			vals.Add("is_deleted", note.isDeleted)
			vals.Add("is_public", note.isPublic)
			vals.Add("is_starred", note.isStarred)
			vals.Add("is_encrypted", false)

			noteUpdatedAt := note.updatedAt
			if markUpdated {
				noteUpdatedAt = now
			}
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
			log.Verbosef("inserted new version of note %d, new version id: %d\n", note.id, versionID)

			//Maybe: could get versions_count as:
			//q := `SELECT count(*) FROM versions WHERE note_id=?`

			// Note: I don't know why I need to explicitly set created_at, but it does get changed
			// to the same value as updated_at when I don't set it here
			q := `
		UPDATE notes SET
		  updated_at=?,
		  created_at=?,
		  content_sha1=?,
		  size=?,
		  format=?,
		  title=?,
		  tags=?,
		  is_public=?,
		  is_deleted=?,
		  is_starred=?,
		  curr_version_id=?,
		  versions_count = versions_count + 1
		WHERE id=?`
			_, err = tx.Exec(q,
				noteUpdatedAt,
				note.createdAt,
				note.contentSha1,
				noteSize,
				note.format,
				note.title,
				serializedTags,
				note.isPublic,
				note.isDeleted,
				note.isStarred,
				versionID,
				note.id)
			if err != nil {
				log.Errorf("tx.Exec('%s') failed with %s\n", q, err)
				return 0, err
			}

			log.Verbosef("updated note with id %d, updated_at: %s, created_at: %s\n", note.id, noteUpdatedAt, note.createdAt)

			err = tx.Commit()
			tx = nil

			return note.id, err
	*/
	return "", nil
}

func dbUpdateNoteWith(userID, noteID string, markUpdated bool, updateFn func(*NewNote) bool) error {
	log.Verbosef("dbUpdateNoteWith: userID=%s, noteID=%s, markUpdated: %v\n", userID, noteID, markUpdated)
	defer clearCachedUserInfo(userID)

	panic("NYI")

	/*
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
		log.Verbosef("dbUpdateNoteWith: note.IsStarred: %v, newNote.isStarred: %v\n", note.IsStarred, newNote.isStarred)

		shouldUpdate := updateFn(newNote)
		if !shouldUpdate {
			log.Verbosef("dbUpdateNoteWith: skipping update of noteID=%s because shouldUpdate=%v\n", hashInt(noteID), shouldUpdate)
			return nil
		}
		_, err = dbUpdateNote2(newNote, markUpdated)
		return err
	*/
	return nil
}

func dbUpdateNoteTitle(userID, noteID string, newTitle string) error {
	return dbUpdateNoteWith(userID, noteID, true, func(newNote *NewNote) bool {
		shouldUpdate := newNote.title != newTitle
		newNote.title = newTitle
		return shouldUpdate
	})
}

func dbUpdateNoteTags(userID, noteID string, newTags []string) error {
	return dbUpdateNoteWith(userID, noteID, true, func(newNote *NewNote) bool {
		shouldUpdate := !strArrEqual(newNote.tags, newTags)
		newNote.tags = newTags
		return shouldUpdate
	})
}

func needsNewNoteVersion(note *NewNote, existingNote *Note) bool {
	/*
		if !bytes.Equal(note.contentSha1, existingNote.ContentSha1) {
			return true
		}
	*/
	if note.title != existingNote.Title {
		return true
	}
	if note.format != existingNote.Format {
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
	if note.isStarred != existingNote.IsStarred {
		return true
	}
	return false
}

func dbGetUsersCount() (int, error) {
	panic("NYI")
	return 0, nil
}

// TODO: remove
func dbGetNotesCount() (int, error) {
	panic("NYI")
	return 0, nil
}

// TODO: remove
func dbGetVersionsCount() (int, error) {
	panic("NYI")
	return 0, nil
}

// get the beginning of the day
// TODO: is there a better way?
func getDayStart(t time.Time) time.Time {
	s := t.Format("2006-01-02")
	dayStart, err := time.Parse("2006-01-02", s)
	u.PanicIfErr(err)
	return dayStart
}

// get start of previous day
func getPrevDayStart() time.Time {
	now := time.Now().UTC()
	now.Add(time.Hour * -24)
	return getDayStart(now)
}

func getCurrDayStart() time.Time {
	return getDayStart(time.Now().UTC())
}

func timeBetween(t, start, end time.Time) bool {
	if start.Sub(t) < 0 {
		return false
	}
	if t.Sub(end) > 0 {
		return false
	}
	return true
}

// create a new note. if note.createdAt is non-zero value, this is an import
// of note from somewhere else, so we want to preserve createdAt value
func dbCreateOrUpdateNote(userID string, note *NewNote) (string, error) {
	log.Verbosef("userID: %s\n", userID)
	var err error

	panic("NYI")

	/*
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

		defer clearCachedUserInfo(userID)

			var noteID int
			var existingNote *Note
			if note.hashID == "" {
				log.Verbosef("creating a new note %s\n", note.title)
				noteID, err = dbCreateNewNote(userID, note)
				note.hashID = hashInt(noteID)
				return noteID, err
			}

			noteID, err = dehashInt(note.hashID)
			if err != nil {
				return 0, err
			}
			existingNote, err = dbGetNoteByID(noteID)
			if err != nil {
				return 0, err
			}
			u.PanicIf(noteID != existingNote.id)
			if existingNote.userID != userID {
				return 0, fmt.Errorf("user %d is trying to update note that belongs to user %d", userID, existingNote.userID)
			}

			note.id = noteID

			// when editing a note, we don't change starred status
			note.isStarred = existingNote.IsStarred
			// don't create new versions if not necessary
			if !needsNewNoteVersion(note, existingNote) {
				return noteID, nil
			}
			log.Verbosef("updating existing note %d (%s). CreatedAt: %s, UpdatedAt: %s\n", existingNote.id, existingNote.HashID, existingNote.CreatedAt.Format(time.RFC3339), existingNote.UpdatedAt.Format(time.RFC3339))

			note.createdAt = existingNote.CreatedAt
			noteID, err = dbUpdateNote2(note, true)

			return noteID, err
	*/
	return "", nil
}

// TODO: also get content_sha1 for each version (requires index on content_sha1
// to be fast) and if this content_sha1 is only referenced by one version,
// delete from google storage
func dbPermanentDeleteNote(userID, noteID string) error {
	defer clearCachedUserInfo(userID)

	panic("NYI")

	return nil
}

func dbDeleteNote(userID, noteID string) error {
	return dbUpdateNoteWith(userID, noteID, true, func(note *NewNote) bool {
		shouldUpdate := !note.isDeleted
		note.isDeleted = true
		return shouldUpdate
	})
}

func dbUndeleteNote(userID, noteID string) error {
	return dbUpdateNoteWith(userID, noteID, true, func(note *NewNote) bool {
		shouldUpdate := note.isDeleted
		note.isDeleted = false
		return shouldUpdate
	})
}

func dbMakeNotePublic(userID, noteID string) error {
	// log.Verbosef("dbMakeNotePublic: userID=%d, noteID=%d", userID, noteID)
	// note: doesn't update lastUpdate for stability of display
	return dbUpdateNoteWith(userID, noteID, false, func(note *NewNote) bool {
		shouldUpdate := !note.isPublic
		note.isPublic = true
		// log.Verbosef(" shouldUpdate=%v\n", shouldUpdate)
		return shouldUpdate
	})
}

func dbMakeNotePrivate(userID, noteID string) error {
	// log.Verbosef("dbMakeNotePrivate: userID: %d, noteID: %d\n", userID, noteID)
	// note: doesn't update lastUpdate for stability of display
	return dbUpdateNoteWith(userID, noteID, false, func(note *NewNote) bool {
		shouldUpdate := note.isPublic
		note.isPublic = false
		return shouldUpdate
	})
}

func dbStarNote(userID, noteID string) error {
	// note: doesn't update lastUpdate for stability of display
	return dbUpdateNoteWith(userID, noteID, false, func(note *NewNote) bool {
		log.Verbosef("dbStarNote: userID: %s, noteID: %s, isStarred: %v\n", userID, noteID, note.isStarred)
		shouldUpdate := !note.isStarred
		note.isStarred = true
		return shouldUpdate
	})
}

func dbUnstarNote(userID, noteID string) error {
	log.Verbosef("dbUnstarNote: userID: %s, noteID: %s\n", userID, noteID)
	// note: doesn't update lastUpdate for stability of display
	return dbUpdateNoteWith(userID, noteID, false, func(note *NewNote) bool {
		shouldUpdate := note.isStarred
		note.isStarred = false
		log.Verbosef("dbUnstarNote: shouldUpdate: %v\n", shouldUpdate)
		return shouldUpdate
	})
}

// note: only use locally for testing search, not in production
func dbGetAllNotes() ([]*Note, error) {
	log.Verbosef("dbGetAllNotes\n")
	panic("NYI")
	var notes []*Note
	return notes, nil
}

func dbGetAllVersionsSha1ForUser(userID string) ([][]byte, error) {
	panic("NYI")
	return nil, nil
}

func dbGetNotesForUser(user *DbUser) ([]*Note, error) {
	panic("NYI")
	return nil, nil
}

var (
	recentPublicNotesCached     []Note
	recentPublicNotesLastUpdate time.Time
)

func timeExpired(t time.Time, dur time.Duration) bool {
	return t.IsZero() || time.Now().Sub(t) > dur
}

func getRecentPublicNotesCached(limit int) ([]Note, error) {
	panic("NYI")
	return nil, nil
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

func dbGetNoteByID(id string) (*Note, error) {
	panic("NYI")
	return nil, nil
}

// id, login, full_name, email, created_at, pro_state
func dbGetUserByQuery(q string, args ...interface{}) (*DbUser, error) {
	panic("NYI")
	return nil, nil
}

func dbGetUserByIDCached(userID string) (*DbUser, error) {
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

func dbGetUserByID(userID string) (*DbUser, error) {
	panic("NYI")
	return nil, nil
}

func dbGetUserByLogin(login string) (*DbUser, error) {
	panic("NYI")
	return nil, nil
}

func dbGetAllUsers() ([]*DbUser, error) {
	panic("NYI")
	return nil, nil
}

func getWelcomeMD() []byte {
	d, err := loadResourceFile(filepath.Join("data", "welcome.md"))
	u.PanicIfErr(err, "getWelcomeMD()")
	return d
}

// TODO: also insert oauthJSON
func dbGetOrCreateUser(userLogin string, fullName string) (*DbUser, error) {
	panic("NYI")
	return nil, nil
}

var (
	firestoreClient *firestore.Client
)

// https://developers.google.com/identity/protocols/googlescopes
const (
	// don't know why this is needed, things were working without
	// but whatever, the docs say so
	ScopeCloudPlatform = "https://www.googleapis.com/auth/cloud-platform"
	ScopeFirestore     = "https://www.googleapis.com/auth/datastore"
)

func getFirestoreClient() (*firestore.Client, error) {
	//muCache.Lock()
	//defer muCache.Unlock()

	if firestoreClient != nil {
		return firestoreClient, nil
	}

	ctx := context.Background()
	d, err := ioutil.ReadFile("service-account.json")
	if err != nil {
		return nil, err
	}
	creds, err := google.CredentialsFromJSON(ctx, d, ScopeFirestore, ScopeCloudPlatform)
	if err != nil {
		return nil, err
	}
	optCreds := option.WithCredentials(creds)
	client, err := firestore.NewClient(ctx, gcpProjectID, optCreds)
	if err != nil {
		return nil, err
	}
	firestoreClient = client
	return firestoreClient, nil
}

const (
	gcpProjectID = "TODO"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func getFirestoreClientMust() *firestore.Client {
	c, err := getFirestoreClient()
	must(err)
	return c
}

func firestoreIsNotExists(err error) bool {
	code := status.Code(err)
	return code == codes.NotFound
}

func nullifyNotFound(err error) error {
	if firestoreIsNotExists(err) {
		return nil
	}
	return err
}
