package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kjk/log"
	"github.com/kjk/simplenote"
)

const (
	simplenoteAPIKey = "b6550d1ac75048988d9007aeae5dda6b"
)

var (
	nextImportID      int
	importsInProgress []*SimpleNoteImport
	muImport          sync.Mutex
)

// ImportedSimpleNote describes an import of simplenote note
type ImportedSimpleNote struct {
	NoteID            int
	SimpleNoteID      string
	SimpleNoteVersion int
}

// ImportCounts describes number of imported/updated/skipped notes
type ImportCounts struct {
	ImportedCount int
	SkippedCount  int
	UpdatedCount  int
}

// SimpleNoteImport describes a state of import
type SimpleNoteImport struct {
	// data owned exclusively by import goroutine
	importID            int
	userID              int
	client              *simplenote.Client
	shouldConvertPublic bool
	alreadyImported     map[string]ImportedSimpleNote
	startedAt           time.Time
	counts              ImportCounts

	// data shared between goroutines, must be locked during access
	ImportCounts // updated from counts
	IsFinished   bool
	Error        string `json:",omitempty"`
	Duration     time.Duration
}

// TODO: call this periodically
func cleanupSimpleNoteImports() {
	muImport.Lock()
again:
	a := importsInProgress
	for i, state := range a {
		shouldRemove := time.Since(state.startedAt) > time.Hour*8
		if shouldRemove {
			// https://github.com/golang/go/wiki/SliceTricks
			importsInProgress, a[len(a)-1] = append(a[:i], a[i+1:]...), nil
			goto again
		}
	}
	muImport.Unlock()
}

func withLockedImport(id int, f func(*SimpleNoteImport)) {
	muImport.Lock()
	defer muImport.Unlock()
	for _, s := range importsInProgress {
		if s.importID == id {
			f(s)
			return
		}
	}
}

func startNewImport(userID int) *SimpleNoteImport {
	muImport.Lock()
	nextImportID++
	importID := nextImportID
	state := &SimpleNoteImport{
		importID:  importID,
		userID:    userID,
		startedAt: time.Now(),
	}
	importsInProgress = append(importsInProgress, state)
	muImport.Unlock()
	return state
}

func getImportStateCopyByID(id int) (SimpleNoteImport, bool) {
	muImport.Lock()
	defer muImport.Unlock()
	for _, s := range importsInProgress {
		if s.importID == id {
			return *s, true
		}
	}
	return SimpleNoteImport{}, false
}

func importUpdateCounts(importID int, counts ImportCounts) {
	withLockedImport(importID, func(status *SimpleNoteImport) {
		status.ImportCounts = counts
		status.Duration = time.Since(status.startedAt)
	})
}

func importSetError(importID int, err string) {
	withLockedImport(importID, func(status *SimpleNoteImport) {
		status.Error = err
		status.IsFinished = true
		status.Duration = time.Since(status.startedAt)
	})
}

func importMarkFinished(importID int) {
	importSetError(importID, "")
}

func isSimpleNoteUnothorizedError(s string) bool {
	// a heuristic
	return strings.Contains(s, "401") && strings.Contains(s, "/authorize/")
}

func snKey(id string, ver int) string {
	return fmt.Sprintf("%s-%d", id, ver)
}

func getSimpleNoteImportsForUser(userID int) (map[string]ImportedSimpleNote, error) {
	db := getDbMust()
	q := `SELECT note_id, simplenote_id, simplenote_version FROM simplenote_imports WHERE user_id = ?`
	rows, err := db.Query(q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var arr []ImportedSimpleNote
	for rows.Next() {
		var sni ImportedSimpleNote
		err = rows.Scan(&sni.NoteID, &sni.SimpleNoteID, &sni.SimpleNoteVersion)
		if err != nil {
			return nil, err
		}
		arr = append(arr, sni)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	res := make(map[string]ImportedSimpleNote)
	for _, sni := range arr {
		key := snKey(sni.SimpleNoteID, sni.SimpleNoteVersion)
		res[key] = sni
	}
	return res, nil
}

func markSimpleNoteImported(state *SimpleNoteImport, noteID int, simplenoteID string, simplenoteVersion int) error {
	db := getDbMust()
	q := `INSERT INTO simplenote_imports (user_id, note_id, simplenote_id, simplenote_version) VALUES (?, ?, ?, ?)`
	_, err := db.Exec(q, state.userID, noteID, simplenoteID, simplenoteVersion)
	if err != nil {
		log.Errorf("db.Exec('%s') (%v, %v, %v, %v) failed with '%s'\n", q, state.userID, noteID, simplenoteID, simplenoteVersion, err)
		return err
	}
	key := snKey(simplenoteID, simplenoteVersion)
	v := ImportedSimpleNote{
		NoteID:            noteID,
		SimpleNoteID:      simplenoteID,
		SimpleNoteVersion: simplenoteVersion,
	}
	state.alreadyImported[key] = v
	return nil
}

func dbIsUserMe(userID int) bool {
	userDb, err := dbGetUserByIDCached(userID)
	if err != nil {
		return false
	}
	switch userDb.Login {
	case "twitter:kjk", "github:kjk", "google:kkowalczyk@gmail.com":
		return true
	}
	return false
}

// other system tags: published, pinned
func isSimpleNoteMarkdown(n *simplenote.Note) bool {
	for _, tag := range n.SystemTags {
		if tag == "markdown" {
			return true
		}
	}
	return false
}

func removePublicTag(tags []string) (bool, []string) {
	for i, tag := range tags {
		if tag == "__public" {
			tags = append(tags[:i], tags[i+1:]...)
			return true, tags
		}
	}
	return false, tags
}

func importSingleNote(state *SimpleNoteImport, note *simplenote.Note) error {
	defer func(s *SimpleNoteImport) {
		importUpdateCounts(s.importID, s.counts)
	}(state)

	snID := note.ID
	snVer := note.Version
	snFullID := snKey(snID, snVer)
	_, ok := state.alreadyImported[snFullID]
	if ok {
		state.counts.SkippedCount++
		log.Verbosef("skipping already imported simplenote %s\n", snFullID)
		return nil
	}
	newNote := NewNote{
		format:    formatText,
		createdAt: note.CreationDate,
		isDeleted: note.IsDeleted,
		tags:      note.Tags,
	}
	noteID := findNoteIDForSimpleNoteID(state.alreadyImported, snID)
	if noteID != -1 {
		log.Verbosef("updating %d with %s\n", noteID, snFullID)
		newNote.idStr = hashInt(noteID)
	}
	if isSimpleNoteMarkdown(note) {
		newNote.format = formatMarkdown
	}
	if state.shouldConvertPublic {
		// convert __public tag to isPublic note state
		newNote.isPublic, newNote.tags = removePublicTag(newNote.tags)
	}
	newNote.tags = append(newNote.tags, "from-simplenote")

	newNote.title, newNote.content = noteToTitleContent([]byte(note.Content))
	if len(newNote.content) == 0 {
		//log.Verbosef("   skipping an empty note\n")
		return nil
	}

	noteID, err := dbCreateOrUpdateNote(state.userID, &newNote)
	if err != nil {
		log.Errorf("dbCreateOrUpdateNote() failed with %s\n", err)
		importSetError(state.importID, fmt.Sprintf("dbCreateOrUpdateNote() failed with %s", err))
		return err
	}
	if newNote.isDeleted {
		log.Verbosef("importing deleted note %s %d, modTime: %s, title: '%s'\n", snFullID, noteID, newNote.createdAt, newNote.title)
	} else {
		log.Verbosef("importing note %s as %d, modTime: %s, title: '%s'\n", snFullID, noteID, newNote.createdAt, newNote.title)
	}
	if ok {
		state.counts.UpdatedCount++
	} else {
		state.counts.ImportedCount++
	}
	return markSimpleNoteImported(state, noteID, note.ID, note.Version)
}

// return -1 if doesn't have the id
func findNoteIDForSimpleNoteID(imported map[string]ImportedSimpleNote, simplenoteID string) int {
	for _, ni := range imported {
		if ni.SimpleNoteID == simplenoteID {
			return ni.NoteID
		}
	}
	return -1
}

func importPreviousVersions(state *SimpleNoteImport, noteLastVer *simplenote.Note) error {
	id := noteLastVer.ID
	// SimpleNote versions start with 1
	for ver := 1; ver < noteLastVer.Version; ver++ {
		note, err := state.client.GetNote(id, ver)
		if err != nil {
			// sometimes a version is not present in SimpleNote
			continue
		}
		err = importSingleNote(state, note)
		if err != nil {
			return err
		}
	}
	return nil
}

func importSimpleNote(state *SimpleNoteImport, email, password string) {
	id := state.importID
	state.shouldConvertPublic = dbIsUserMe(state.userID)
	// for now only import previous versions for me
	// Maybe: enable for everyone with a checkbox in import dialog
	importPrevious := dbIsUserMe(state.userID)
	state.client = simplenote.NewClient(simplenoteAPIKey, email, password)
	notes, err := state.client.List()
	if err != nil {
		log.Errorf("c.List() failed with '%s'\n", err)
		msg := fmt.Sprintf("c.List() failed with '%s'", err)
		if isSimpleNoteUnothorizedError(err.Error()) {
			msg = "Authentication failed. Invalid email or password"
		}
		importSetError(id, msg)
		return
	}

	state.alreadyImported, err = getSimpleNoteImportsForUser(state.userID)
	if err != nil {
		log.Errorf("getSimpleNoteImportsForUser() failed with '%s'\n", err)
		importSetError(id, err.Error())
		return
	}

	for _, note := range notes {
		if importPrevious {
			err = importPreviousVersions(state, note)
			if err != nil {
				break
			}
		}
		err = importSingleNote(state, note)
		if err != nil {
			break
		}
	}
	importUpdateCounts(state.importID, state.counts)
	importMarkFinished(id)
	// free up large resources
	state.client = nil
	state.alreadyImported = nil
}

/* GET /api/import_simplenote_status
args:
  id      : import id
result:
  {
    ImportedCount: 5,
    IsFinished; true,
    Error: "",
    Duration: "5 secs"
  }
*/
func handleAPIImportSimpleNotesStatus(w http.ResponseWriter, r *http.Request) {
	log.Verbosef("url: '%s'\n", r.URL.Path)
	dbUser := getDbUserFromCookie(w, r)
	if dbUser == nil {
		httpErrorWithJSONf(w, "not logged in")
		return
	}
	idStr := strings.TrimSpace(r.FormValue("id"))
	id, err := strconv.Atoi(idStr)
	if err != nil {
		httpErrorWithJSONf(w, "missing or invalid id value ('%s')", idStr)
		return
	}
	status, ok := getImportStateCopyByID(id)
	if !ok {
		httpErrorWithJSONf(w, "no ipmort with id %d", id)
		return
	}
	if status.userID != dbUser.ID {
		log.Errorf("status.userID=%d, dbUser.ID=%d\n", status.userID, dbUser.ID)
		httpErrorWithJSONf(w, "not the right user")
		return
	}
	if !status.IsFinished {
		status.Duration = time.Since(status.startedAt)
	}
	httpOkWithJSON(w, r, status)
}

/* GET /api/import_simplenote_start
args:
  email   : simplenote user e-email
  assword : simplenote user password
result:
  {
    ImportID: 5
  }
*/
func handleAPIImportSimpleNoteStart(w http.ResponseWriter, r *http.Request) {
	log.Verbosef("url: '%s'\n", r.URL.Path)
	dbUser := getDbUserFromCookie(w, r)
	if dbUser == nil {
		httpErrorWithJSONf(w, "not logged in")
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	password := strings.TrimSpace(r.FormValue("password"))
	if email == "" || password == "" {
		httpErrorWithJSONf(w, "Missing email or password.")
		return
	}
	log.Verbosef("importing for user: %s, email: '%s', pwd: '%s'\n", dbUser.Login, email, password)
	state := startNewImport(dbUser.ID)
	id := state.importID
	go importSimpleNote(state, email, password)
	v := struct {
		ImportID int
	}{
		ImportID: id,
	}
	httpOkWithJSON(w, r, v)
}
