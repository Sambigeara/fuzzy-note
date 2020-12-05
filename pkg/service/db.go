package service

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
)

// DBHandler currently not really achieving anything as an interface as uses are
// quite unique, but YOLO
//type DBHandler interface {
//    read()
//    write()
//}

type schemaMap map[uint16]interface{}

func getSchema(schemaMap schemaMap, fileSchemaID uint16) interface{} {
	i, ok := schemaMap[fileSchemaID]
	if !ok {
		i, _ = schemaMap[0]
	}
	return i
}

//
// Main ListItem file handling
//

// ListItemDBHandler implements the DBHandler schema
type ListItemDBHandler struct {
	schemaMap          schemaMap
	latestFileSchemaID uint16
	notesPath          string // TODO remove after file Schema > 0
}

// NewListItemDBHandler returns an instance of ListItemDBHandler
func NewListItemDBHandler(notesPath string) *ListItemDBHandler {
	return &ListItemDBHandler{
		schemaMap: map[uint16]interface{}{
			0: listItemSchema0{},
			1: listItemSchema1{},
		},
		latestFileSchemaID: 1,
		notesPath:          notesPath,
	}
}

type fileHeader struct {
	FileSchemaID uint16
}

type listItemSchema0 struct {
	PageID     uint32
	Metadata   bits
	FileID     uint32
	LineLength uint64
}
type listItemSchema1 struct {
	PageID     uint32
	Metadata   bits
	LineLength uint64
	NoteLength uint64
}

func (h *ListItemDBHandler) read(f io.Reader, fileSchemaID uint16, newItem *ListItem) (bool, error) {
	i := getSchema(h.schemaMap, fileSchemaID)

	var err error
	switch s := i.(type) {
	case listItemSchema0:
		err = binary.Read(f, binary.LittleEndian, &s)
		if err != nil {
			switch err {
			case io.EOF:
				return false, nil
			case io.ErrUnexpectedEOF:
				fmt.Println("binary.Read failed on listItem header:", err)
				return false, err
			}
		}

		line := make([]byte, s.LineLength)
		err = binary.Read(f, binary.LittleEndian, &line)
		if err != nil {
			fmt.Println("binary.Read failed on listItem line:", err)
			return false, err
		}

		note, err := h.loadPage(s.PageID)
		if err != nil {
			return false, err
		}

		newItem.Line = string(line)
		newItem.id = s.PageID
		newItem.Note = note
		newItem.IsHidden = has(s.Metadata, hidden)
		return true, nil
	case listItemSchema1:
		err = binary.Read(f, binary.LittleEndian, &s)
		if err != nil {
			switch err {
			case io.EOF:
				return false, nil
			case io.ErrUnexpectedEOF:
				fmt.Println("binary.Read failed on listItem header:", err)
				return false, err
			}
		}

		line := make([]byte, s.LineLength)
		err = binary.Read(f, binary.LittleEndian, &line)
		if err != nil {
			fmt.Println("binary.Read failed on listItem line:", err)
			return false, err
		}

		note := make([]byte, s.NoteLength)
		err = binary.Read(f, binary.LittleEndian, note)
		if err != nil {
			fmt.Println("binary.Read failed on listItem note:", err)
			return false, err
		}

		newItem.Line = string(line)
		newItem.id = s.PageID
		newItem.Note = note
		newItem.IsHidden = has(s.Metadata, hidden)
		return true, nil
	}
	return false, err
}

func (h *ListItemDBHandler) write(f io.Writer, listItem *ListItem) error {
	i := getSchema(h.schemaMap, h.latestFileSchemaID)

	var err error
	switch s := i.(type) {
	case listItemSchema0:
		var metadata bits = 0
		if listItem.IsHidden {
			metadata = set(metadata, hidden)
		}

		s.PageID = listItem.id
		s.Metadata = metadata
		s.FileID = listItem.id
		s.LineLength = uint64(len([]byte(listItem.Line)))

		byteLine := []byte(listItem.Line)

		data := []interface{}{&s, &byteLine}

		// TODO the below writes need to be atomic
		for _, v := range data {
			err = binary.Write(f, binary.LittleEndian, v)
			if err != nil {
				fmt.Printf("binary.Write failed when writing %v: %s\n", v, err)
				log.Fatal(err)
				return err
			}
		}

		h.savePage(listItem.id, listItem.Note)
	case listItemSchema1:
		var metadata bits = 0
		if listItem.IsHidden {
			metadata = set(metadata, hidden)
		}
		byteLine := []byte(listItem.Line)

		s.PageID = listItem.id
		s.Metadata = metadata
		s.LineLength = uint64(len([]byte(listItem.Line)))
		s.NoteLength = 0

		data := []interface{}{&s, &byteLine}
		if listItem.Note != nil {
			s.NoteLength = uint64(len(listItem.Note))
			data = append(data, listItem.Note)
		}

		// TODO the below writes need to be atomic
		for _, v := range data {
			err = binary.Write(f, binary.LittleEndian, v)
			if err != nil {
				fmt.Printf("binary.Write failed when writing %v: %s\n", v, err)
				log.Fatal(err)
				return err
			}
		}
	}
	return err
}

// TODO these can be deleted when no more legacy file Schema users
func (h *ListItemDBHandler) loadPage(id uint32) ([]byte, error) {
	strID := fmt.Sprint(id)
	filePath := path.Join(h.notesPath, strID)

	dat := make([]byte, 0)
	// If file does not exist, return nil
	if _, err := os.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			return dat, nil
		} else {
			return nil, err
		}
	}

	// Read whole file
	dat, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Fatal(err)
		return nil, err
	}
	return dat, nil
}

func (h *ListItemDBHandler) savePage(id uint32, data []byte) error {
	strID := fmt.Sprint(id)
	filePath := path.Join(h.notesPath, strID)

	// If data has been removed or is empty, delete the file and return
	if data == nil || len(data) == 0 {
		_ = os.Remove(filePath)
		// TODO handle failure more gracefully? AFAIK os.Remove just returns a *PathError on failure
		// which is mostly indicative of a noneexistent file, so good enough for now...
		return nil
	}

	// Open or create a file in the `/notes/` subdir using the listItem id as the file name
	// This needs to be before the ReadFile below to ensure the file exists
	f, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	defer f.Close()

	_, err = f.Write(data)
	if err != nil {
		fmt.Println("binary.Write failed:", err)
		return err
	}
	return nil
}

//
// Undo/Redo Eventlog file handling
//

// EventLogDBHandler implements the DBHandler schema
type EventLogDBHandler struct {
	schemaMap              schemaMap
	latestEventLogSchemaID uint16
	rootPath               string
	curIdx                 int // Last index is latest/most recent in history (appends on new events)
	log                    []event
}

// NewEventLogDBHandler returns an instance of ListItemDBHandler
func NewEventLogDBHandler(rootPath string) *EventLogDBHandler {
	el := event{
		eventType: nullEvent,
		ptr:       nil,
		undoLine:  "",
		undoNote:  nil,
		redoLine:  "",
		redoNote:  nil,
	}
	return &EventLogDBHandler{
		schemaMap: map[uint16]interface{}{
			0: eventLogSchema0{},
		},
		latestEventLogSchemaID: 0,
		rootPath:               rootPath,
		curIdx:                 0,
		log:                    []event{el},
	}
}

type eventLogHeader struct {
	EventLogSchemaID uint16
}

type eventLogSchema0 struct {
	EventType  []byte
	ListItemID uint32
	UndoLine   []byte
	UndoNote   []byte
	RedoLine   []byte
	RedoNote   []byte
}

func (h *EventLogDBHandler) read(listItemMap map[uint32]*ListItem) error {
	f, err := os.Open(h.rootPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var eventLogSchemaID uint16
	err = binary.Read(f, binary.LittleEndian, eventLogSchemaID)
	if err != nil {
		switch err {
		case io.EOF:
			return nil
		case io.ErrUnexpectedEOF:
			fmt.Println("binary.Read failed on listItem header:", err)
			return err
		}
	}

	i := getSchema(h.schemaMap, eventLogSchemaID)
	s := i.(eventLogSchema0)

	for {
		err = binary.Read(f, binary.LittleEndian, &s)
		if err != nil {
			switch err {
			case io.EOF:
				return nil
			case io.ErrUnexpectedEOF:
				fmt.Println("binary.Read failed on listItem header:", err)
				return err
			}
		}
		ptr, ok := listItemMap[s.ListItemID]
		if !ok {
			return errors.New("eventLog requests nonexistent listItem")
		}
		ev := event{
			eventType: eventType(s.EventType),
			ptr:       ptr,
			undoLine:  string(s.UndoLine),
			undoNote:  s.UndoNote,
			redoLine:  string(s.RedoLine),
			redoNote:  s.RedoNote,
		}
		h.log = append(h.log, ev)
	}
}

func (h *EventLogDBHandler) write() error {
	f, err := os.Create(h.rootPath)
	if err != nil {
		log.Fatal(err)
		return err
	}
	defer f.Close()

	// There is a default nullEvent in the log which we can ignore
	if len(h.log) == 1 {
		return nil
	}

	// Write eventLogSchemaID
	err = binary.Write(f, binary.LittleEndian, h.latestEventLogSchemaID)
	if err != nil {
		log.Fatal(err)
		return err
	}

	//i := getSchema(h.schemaMap, h.latestEventLogSchemaID)
	//s := i.(eventLogSchema0)

	for _, e := range h.log {
		if e.eventType == nullEvent {
			continue
		}
		//l := s

		//l.EventType = []byte(e.eventType)
		//l.ListItemID = e.ptr.id
		//l.UndoLine = []byte(e.undoLine)
		//l.UndoNote = e.undoNote
		//l.RedoLine = []byte(e.redoLine)
		//l.RedoNote = e.redoNote

		data := []interface{}{
			[]byte(e.eventType),
			e.ptr.id,
			[]byte(e.undoLine),
			e.undoNote,
			[]byte(e.redoLine),
			e.redoNote,
		}

		for _, v := range data {
			err = binary.Write(f, binary.LittleEndian, v)
			if err != nil {
				fmt.Printf("binary.Write failed when writing %v: %s\n", e, err)
				log.Fatal(err)
				return err
			}
		}
	}
	return nil
}
