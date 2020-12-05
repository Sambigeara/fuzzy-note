package service

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"strings"
	"time"
	"unicode"
)

// This is THE date that Golang needs to determine custom formatting
const dateFormat string = "Mon, Jan 2, 2006"

// ListRepo represents the main interface to the data store backend
type ListRepo interface {
	Load() error
	Save() error
	Add(line string, note []byte, childItem *ListItem, newItem *ListItem) error
	Update(line string, note []byte, listItem *ListItem) error
	Delete(listItem *ListItem) error
	MoveUp(listItem *ListItem) (bool, error)
	MoveDown(listItem *ListItem) (bool, error)
	Undo() error
	Redo() error
	Match(keys [][]rune, active *ListItem, showHidden bool) ([]*ListItem, error)
	GetMatchPattern(sub []rune) (matchPattern, int)
}

// DBListRepo is an implementation of the ListRepo interface
type DBListRepo struct {
	rootPath          string
	root              *ListItem
	nextID            uint32
	pendingDeletions  []*ListItem
	listItemDBHandler *ListItemDBHandler
	eventLogDBHandler *EventLogDBHandler
}

// ListItem represents a single item in the returned list, based on the Match() input
type ListItem struct {
	Line        string
	Note        []byte
	IsHidden    bool
	child       *ListItem
	parent      *ListItem
	id          uint32
	matchChild  *ListItem
	matchParent *ListItem
}

type bits uint32

const (
	hidden bits = 1 << iota
)

func set(b, flag bits) bits    { return b | flag }
func clear(b, flag bits) bits  { return b &^ flag }
func toggle(b, flag bits) bits { return b ^ flag }
func has(b, flag bits) bool    { return b&flag != 0 }

// NewDBListRepo returns a pointer to a new instance of DBListRepo
func NewDBListRepo(rootPath string, listItemDBHandler *ListItemDBHandler, eventLogDBHandler *EventLogDBHandler) *DBListRepo {
	return &DBListRepo{
		rootPath:          rootPath,
		nextID:            1,
		listItemDBHandler: listItemDBHandler,
		eventLogDBHandler: eventLogDBHandler,
	}
}

// Load is called on initial startup. It instantiates the app, and deserialises and displays
// default LineItems
func (r *DBListRepo) Load() error {
	f, err := os.OpenFile(r.rootPath, os.O_CREATE, 0644)
	if err != nil {
		log.Fatal(err)
		return err
	}
	defer f.Close()

	// TODO remove this temp measure once all users are on file schema >= 1
	// The first version did not have a file schema. To detemine if a file is of the original schema,
	// we rely on the likely fact that no-one has generated enough listItems (>65535) to use the right two
	// bytes of the initial uint32 assigned for the first listItemID. E.g. if the second uint16 (bytes 3/4)
	// are 0, then it's likely the original schema.
	// With the above in mind, we can infer the correct file schema and file offset as follows:
	//
	// if first uint16 == 0:
	//   fileSchemaId = 0
	//   fileOffset = 2
	// else if second uint16 == 0:
	//   fileSchemaId = 0
	//   fileOffset = 0
	// else:
	//   fileSchemaId = first uint16
	//   fileOffset = 2
	//
	var firstTwo, secondTwo uint16
	err = binary.Read(f, binary.LittleEndian, &firstTwo)
	err = binary.Read(f, binary.LittleEndian, &secondTwo)
	if err != nil {
		if err == io.EOF {
			return nil
		}
		log.Fatal(err)
		return err
	}
	// NOTE: File offset now at 4

	fileHeader := fileHeader{}
	wasUnversionedFile := false
	if firstTwo == 0 {
		// File schema is explicitly set to 0
		fileHeader.FileSchemaID = 0
		f.Seek(2, io.SeekStart)
	} else if secondTwo == 0 {
		wasUnversionedFile = true
		// Fileschema not set (assuming no lineItem with ID > 65535)
		fileHeader.FileSchemaID = 0
		f.Seek(0, io.SeekStart)
	} else {
		fileHeader.FileSchemaID = firstTwo
		f.Seek(2, io.SeekStart)
	}

	// Retrieve first line from the file, which will be the youngest (and therefore top) entry
	var cur *ListItem
	listItemMap := make(map[uint32]*ListItem)

	for {
		nextItem := ListItem{}
		cont, err := r.listItemDBHandler.read(f, fileHeader.FileSchemaID, &nextItem)
		if err != nil {
			//log.Fatal(err)
			return err
		}
		listItemMap[nextItem.id] = &nextItem
		// TODO: 2020-12-05 remove once everyone using file schema >= 1
		// Under normal circumstances, the first item read from the file will be youngest.
		// However, due to annoying historical "reasons", the first non-versioned file schema
		// wrote the listItems in reverse order, so we need to add an awful TEMP hack in here
		// to join the doubly linked list in the reverse order
		if wasUnversionedFile {
			// This bit will be removed
			nextItem.parent = cur
			if !cont {
				r.root = cur

				// Write eventLog
				r.eventLogDBHandler.read(listItemMap)

				return nil
			}
			if cur != nil {
				cur.child = &nextItem
			}
			cur = &nextItem
		} else {
			// This bit will stay
			nextItem.child = cur
			if !cont {
				// Write eventLog
				r.eventLogDBHandler.read(listItemMap)

				return nil
			}
			if cur == nil {
				r.root = &nextItem
			} else {
				cur.parent = &nextItem
			}
			cur = &nextItem
		}

		// We need to find the next available index for the entire dataset
		if nextItem.id >= r.nextID {
			r.nextID = nextItem.id + 1
		}
	}
}

// Save is called on app shutdown. It flushes all state changes in memory to disk
func (r *DBListRepo) Save() error {
	// TODO remove all files starting with `bak_*`, these are no longer needed

	// Write eventLog
	r.eventLogDBHandler.write()

	// Delete any files that need clearing up
	for _, item := range r.pendingDeletions {
		strID := fmt.Sprint(item.id)
		oldPath := path.Join(r.listItemDBHandler.notesPath, strID)
		err := os.Remove(oldPath)
		if err != nil {
			// TODO is this required?
			if !os.IsNotExist(err) {
				log.Fatal(err)
				return err
			}
		}
	}

	f, err := os.Create(r.rootPath)
	if err != nil {
		log.Fatal(err)
		return err
	}
	defer f.Close()

	// Return if no files to write. os.Create truncates by default so the file will
	// have been overwritten
	if r.root == nil {
		return nil
	}

	// Write the file schema id to the start of the file
	err = binary.Write(f, binary.LittleEndian, r.listItemDBHandler.latestFileSchemaID)
	if err != nil {
		fmt.Println("binary.Write failed when writing fileSchemaID:", err)
		log.Fatal(err)
		return err
	}

	cur := r.root

	for {
		err := r.listItemDBHandler.write(f, cur)
		if err != nil {
			//log.Fatal(err)
			return err
		}

		if cur.parent == nil {
			break
		}
		cur = cur.parent
	}
	return nil
}

func (r *DBListRepo) add(line string, note []byte, childItem *ListItem, newItem *ListItem) (*ListItem, error) {
	if note == nil {
		note = []byte{}
	}
	if newItem == nil {
		newItem = &ListItem{
			Line:  line,
			id:    r.nextID,
			child: childItem,
			Note:  note,
		}
	}
	r.nextID++

	// If `child` is nil, it's the first item in the list so set as root and return
	if childItem == nil {
		oldRoot := r.root
		r.root = newItem
		if oldRoot != nil {
			newItem.parent = oldRoot
			oldRoot.child = newItem
		}
		return newItem, nil
	}

	if childItem.parent != nil {
		childItem.parent.child = newItem
		newItem.parent = childItem.parent
	}
	childItem.parent = newItem

	return newItem, nil
}

// Update will update the line or note of an existing ListItem
func (r *DBListRepo) update(line string, note []byte, listItem *ListItem) error {
	line = r.parseOperatorGroups(line)
	listItem.Line = line
	listItem.Note = note
	return nil
}

func (r *DBListRepo) del(item *ListItem) error {
	if item.child != nil {
		item.child.parent = item.parent
	} else {
		// If the item has no child, it is at the top of the list and therefore we need to update the root
		r.root = item.parent
	}

	if item.parent != nil {
		item.parent.child = item.child
	}

	r.pendingDeletions = append(r.pendingDeletions, item)

	return nil
}

func (r *DBListRepo) moveItem(item *ListItem, newChild *ListItem, newParent *ListItem) error {
	// Close off gap from source location (for whole dataset)
	if item.child != nil {
		item.child.parent = item.parent
	}
	if item.parent != nil {
		item.parent.child = item.child
	}

	// Insert item into new position based on Matched pointers
	item.child = newChild
	item.parent = newParent

	// Update pointers at target location
	if newParent != nil {
		newParent.child = item
	}
	if newChild != nil {
		newChild.parent = item
	}

	// Update root if required
	for r.root.child != nil {
		r.root = r.root.child
	}
	return nil
}

func (r *DBListRepo) moveUp(item *ListItem) (bool, error) {
	targetItem := item.matchChild
	if targetItem == nil {
		return false, nil
	}

	newChild := targetItem.child
	newParent := targetItem
	err := r.moveItem(item, newChild, newParent)
	return true, err
}

func (r *DBListRepo) moveDown(item *ListItem) (bool, error) {
	targetItem := item.matchParent
	if targetItem == nil {
		return false, nil
	}

	newChild := targetItem
	newParent := targetItem.parent
	err := r.moveItem(item, newChild, newParent)
	return true, err
}

func (r *DBListRepo) incrementEventLog() {
	r.eventLogDBHandler.curIdx++
	// Truncate the event log, so when we Undo and then do something new, the previous Redo events
	// are overwritten
	r.eventLogDBHandler.log = r.eventLogDBHandler.log[:r.eventLogDBHandler.curIdx+1]
}

// Add adds a new LineItem with string, note and a pointer to the child LineItem for positioning
func (r *DBListRepo) Add(line string, note []byte, childItem *ListItem, newItem *ListItem) error {
	newItem, err := r.add(line, note, childItem, newItem)
	r.eventLogDBHandler.addLog(addEvent, newItem, "", nil)
	r.incrementEventLog()
	return err
}

// Update will update the line or note of an existing ListItem
func (r *DBListRepo) Update(line string, note []byte, listItem *ListItem) error {
	r.eventLogDBHandler.addLog(updateEvent, listItem, line, note)
	r.incrementEventLog()
	return r.update(line, note, listItem)
}

// Delete will remove an existing ListItem
func (r *DBListRepo) Delete(item *ListItem) error {
	r.eventLogDBHandler.addLog(deleteEvent, item, "", nil)
	r.incrementEventLog()
	return r.del(item)
}

// MoveUp will swop a ListItem with the ListItem directly above it, taking visibility and
// current matches into account.
func (r *DBListRepo) MoveUp(item *ListItem) (bool, error) {
	r.eventLogDBHandler.addLog(moveUpEvent, item, "", nil)
	r.incrementEventLog()
	return r.moveUp(item)
}

// MoveDown will swop a ListItem with the ListItem directly below it, taking visibility and
// current matches into account.
func (r *DBListRepo) MoveDown(item *ListItem) (bool, error) {
	r.eventLogDBHandler.addLog(moveDownEvent, item, "", nil)
	r.incrementEventLog()
	return r.moveDown(item)
}

func (r *DBListRepo) callFunctionForEventLog(ev eventType, ptr *ListItem, line string, note []byte) error {
	var err error
	switch ev {
	case addEvent:
		_, err = r.add(ptr.Line, ptr.Note, ptr.child, ptr)
	case deleteEvent:
		err = r.del(ptr)
	case updateEvent:
		err = r.update(line, note, ptr)
	case moveUpEvent:
		_, err = r.moveUp(ptr)
	case moveDownEvent:
		_, err = r.moveDown(ptr)
	}
	return err
}

func (r *DBListRepo) Undo() error {
	if r.eventLogDBHandler.curIdx > 0 {
		event := r.eventLogDBHandler.log[r.eventLogDBHandler.curIdx]
		// callFunctionForEventLog needs to call the appropriate function with the
		// necessary parameters to reverse the operation
		opEv := oppositeEvent[event.eventType]
		err := r.callFunctionForEventLog(opEv, event.ptr, event.undoLine, event.undoNote)
		r.eventLogDBHandler.curIdx--
		return err
	}
	return nil
}

func (r *DBListRepo) Redo() error {
	// Redo needs to look forward +1 index when actioning events
	if r.eventLogDBHandler.curIdx < len(r.eventLogDBHandler.log)-1 {
		event := r.eventLogDBHandler.log[r.eventLogDBHandler.curIdx+1]
		err := r.callFunctionForEventLog(event.eventType, event.ptr, event.redoLine, event.redoNote)
		r.eventLogDBHandler.curIdx++
		return err
	}
	return nil
}

// Search functionality

func isSubString(sub string, full string) bool {
	if strings.Contains(strings.ToLower(full), strings.ToLower(sub)) {
		return true
	}
	return false
}

// Iterate through the full string, when you match the "head" of the sub rune slice,
// pop it and continue through. If you clear sub, return true. Searches in O(n)
func isFuzzyMatch(sub []rune, full string) bool {
	for _, c := range full {
		if unicode.ToLower(c) == unicode.ToLower(sub[0]) {
			_, sub = sub[0], sub[1:]
		}
		if len(sub) == 0 {
			return true
		}
	}
	return false
}

const (
	openOp  rune = '{'
	closeOp rune = '}'
)

type matchPattern int

const (
	fullMatchPattern matchPattern = iota
	inverseMatchPattern
	fuzzyMatchPattern
	noMatchPattern
)

// matchChars represents the number of characters at the start of the string
// which are attributed to the match pattern.
// This is used elsewhere to strip the characters where appropriate
var matchChars = map[matchPattern]int{
	fullMatchPattern:    1,
	inverseMatchPattern: 2,
	fuzzyMatchPattern:   0,
	noMatchPattern:      0,
}

// GetMatchPattern will return the matchPattern of a given string, if any, plus the number
// of chars that can be omitted to leave only the relevant text
func (r *DBListRepo) GetMatchPattern(sub []rune) (matchPattern, int) {
	if len(sub) == 0 {
		return noMatchPattern, 0
	}
	pattern := fuzzyMatchPattern
	if sub[0] == '#' {
		pattern = fullMatchPattern
		if len(sub) > 1 {
			// Inverse string match if a search group begins with `#!`
			if sub[1] == '!' {
				pattern = inverseMatchPattern
			}
		}
	}
	nChars, _ := matchChars[pattern]
	return pattern, nChars
}

func (r *DBListRepo) parseOperatorGroups(sub string) string {
	// Match the op against any known operator (e.g. date) and parse if applicable.
	// TODO for now, just match `d` or `D` for date, we'll expand in the future.
	now := time.Now()
	dateString := now.Format(dateFormat)
	sub = strings.ReplaceAll(sub, "{d}", dateString)
	return sub
}

// If a matching group starts with `#` do a substring match, otherwise do a fuzzy search
func isMatch(sub []rune, full string, pattern matchPattern) bool {
	if len(sub) == 0 {
		return true
	}
	switch pattern {
	case fullMatchPattern:
		return isSubString(string(sub), full)
	case inverseMatchPattern:
		return !isSubString(string(sub), full)
	case fuzzyMatchPattern:
		return isFuzzyMatch(sub, full)
	default:
		// Shouldn't reach here
		return false
	}
}

// Match takes a set of search groups and applies each to all ListItems, returning those that
// fulfil all rules.
func (r *DBListRepo) Match(keys [][]rune, active *ListItem, showHidden bool) ([]*ListItem, error) {
	// For each line, iterate through each searchGroup. We should be left with lines with fulfil all groups

	// We need to pre-process the keys to parse any operators. We can't do this in the same loop as when
	// we have no matching lines, the parsing logic will not be reached, and things get messy
	for i, group := range keys {
		group = []rune(r.parseOperatorGroups(string(group)))
		// TODO Confirm: The slices within the slice appear to be the same mem locations as those
		// passed in so they mutate as needed
		keys[i] = group
	}

	cur := r.root
	var lastCur *ListItem

	res := []*ListItem{}

	if cur == nil {
		return res, nil
	}

	for {
		// Nullify match pointers
		// TODO centralise this logic, it's too closely coupled with the moveItem logic (if match pointers
		// aren't cleaned up between ANY ops, it can lead to weird behaviour as things operate based on
		// the existence and setting of them)
		cur.matchChild, cur.matchParent = nil, nil

		if showHidden || !cur.IsHidden {
			matched := true
			for _, group := range keys {
				// Match any items with empty Lines (this accounts for lines added when search is active)
				// "active" listItems pass automatically to allow mid-search item editing
				if len(cur.Line) == 0 || cur == active {
					break
				}
				// TODO unfortunate reuse of vars - refactor to tidy
				pattern, nChars := r.GetMatchPattern(group)
				if !isMatch(group[nChars:], cur.Line, pattern) {
					matched = false
					break
				}
			}
			if matched {
				res = append(res, cur)

				if lastCur != nil {
					lastCur.matchParent = cur
				}
				cur.matchChild = lastCur
				lastCur = cur
			}
		}

		if cur.parent == nil {
			return res, nil
		}

		cur = cur.parent
	}
}
