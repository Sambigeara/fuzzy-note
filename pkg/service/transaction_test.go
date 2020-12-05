package service

import (
	//"fmt"
	"os"
	"testing"
)

func TestTransactionUndo(t *testing.T) {
	t.Run("Undo on empty db", func(t *testing.T) {
		rootPath := "file_to_delete"
		mockListRepo := NewDBListRepo(rootPath, NewListItemDBHandler(""), NewEventLogDBHandler(""))
		defer os.Remove(rootPath)

		err := mockListRepo.Undo()
		if err != nil {
			t.Fatal(err)
		}

		if len(mockListRepo.eventLogDBHandler.log) != 1 {
			t.Errorf("Event log should instantiate with a null event log at idx zero")
		}

		matches, _ := mockListRepo.Match([][]rune{}, nil, true)

		if len(matches) != 0 {
			t.Errorf("Undo should have done nothing")
		}
	})
	t.Run("Undo single item Add", func(t *testing.T) {
		rootPath := "file_to_delete"
		mockListRepo := NewDBListRepo(rootPath, NewListItemDBHandler(""), NewEventLogDBHandler(""))
		defer os.Remove(rootPath)

		line := "New item"
		mockListRepo.Add(line, nil, nil, nil)

		if len(mockListRepo.eventLogDBHandler.log) != 2 {
			t.Errorf("Event log should have one null and one real event in it")
		}

		matches, _ := mockListRepo.Match([][]rune{}, nil, true)

		logItem := mockListRepo.eventLogDBHandler.log[1]
		if logItem.eventType != addEvent {
			t.Errorf("Event log entry should be of type AddEvent")
		}
		if logItem.ptr.id != matches[0].id {
			t.Errorf("Event log list item should have the same id")
		}
		if (mockListRepo.eventLogDBHandler.curIdx) != 1 {
			t.Errorf("The event logger index should increment to 1")
		}

		err := mockListRepo.Undo()
		if err != nil {
			t.Fatal(err)
		}

		matches, _ = mockListRepo.Match([][]rune{}, nil, true)

		if len(matches) != 0 {
			t.Errorf("Undo should have removed the only item")
		}
		if mockListRepo.root != nil {
			t.Errorf("The root should have been cleared")
		}

		if len(mockListRepo.eventLogDBHandler.log) != 2 {
			t.Errorf("Event logger should persist the log")
		}
		if (mockListRepo.eventLogDBHandler.curIdx) != 0 {
			t.Errorf("The event logger index should decrement back to 0")
		}
	})
	t.Run("Undo single item Add and Update", func(t *testing.T) {
		rootPath := "file_to_delete"
		mockListRepo := NewDBListRepo(rootPath, NewListItemDBHandler(""), NewEventLogDBHandler(""))
		defer os.Remove(rootPath)

		line := "New item"
		mockListRepo.Add(line, nil, nil, nil)

		updatedLine := "Updated item"
		matches, _ := mockListRepo.Match([][]rune{}, nil, true)
		mockListRepo.Update(updatedLine, []byte{}, matches[0])

		if len(mockListRepo.eventLogDBHandler.log) != 3 {
			t.Errorf("Event log should have one null and two real events in it")
		}
		if mockListRepo.eventLogDBHandler.curIdx != 2 {
			t.Errorf("Event logger should be at position two")
		}

		newestLogItem := mockListRepo.eventLogDBHandler.log[2]
		if newestLogItem.eventType != updateEvent {
			t.Errorf("Newest event log entry should be of type UpdateEvent")
		}
		if newestLogItem.undoLine != line {
			t.Errorf("Newest event log list item should have the original line")
		}

		oldestLogItem := mockListRepo.eventLogDBHandler.log[1]
		if oldestLogItem.eventType != addEvent {
			t.Errorf("Oldest event log entry should be of type AddEvent")
		}

		matches, _ = mockListRepo.Match([][]rune{}, nil, true)
		if matches[0].Line != updatedLine {
			t.Errorf("List item should have the updated line")
		}

		err := mockListRepo.Undo()
		if err != nil {
			t.Fatal(err)
		}

		if len(mockListRepo.eventLogDBHandler.log) != 3 {
			t.Errorf("Event log should still have three events in it")
		}
		if mockListRepo.eventLogDBHandler.curIdx != 1 {
			t.Errorf("Event logger should have decremented to one")
		}

		matches, _ = mockListRepo.Match([][]rune{}, nil, true)
		if len(matches) != 1 {
			t.Errorf("Undo should have updated the item, not deleted it")
		}
		if matches[0].Line != line {
			t.Errorf("List item should now have the original line")
		}

		err = mockListRepo.Undo()
		if err != nil {
			t.Fatal(err)
		}

		if len(mockListRepo.eventLogDBHandler.log) != 3 {
			t.Errorf("Event log should still have three events in it")
		}
		if mockListRepo.eventLogDBHandler.curIdx != 0 {
			t.Errorf("Event logger should have decremented to zero")
		}

		matches, _ = mockListRepo.Match([][]rune{}, nil, true)
		if len(matches) != 0 {
			t.Errorf("Second undo should have deleted the item")
		}
		if mockListRepo.root != nil {
			t.Errorf("Undo should have removed the root")
		}
	})
	t.Run("Add twice, Delete twice, Undo twice, Redo once", func(t *testing.T) {
		rootPath := "file_to_delete"
		mockListRepo := NewDBListRepo(rootPath, NewListItemDBHandler(""), NewEventLogDBHandler(""))
		defer os.Remove(rootPath)

		line := "New item"
		mockListRepo.Add(line, nil, nil, nil)

		if len(mockListRepo.eventLogDBHandler.log) != 2 {
			t.Errorf("Event log should have one null and one real event in it")
		}
		if mockListRepo.eventLogDBHandler.curIdx != 1 {
			t.Errorf("Event logger should have incremented to one")
		}

		logItem := mockListRepo.eventLogDBHandler.log[1]
		if logItem.eventType != addEvent {
			t.Errorf("Event log entry should be of type AddEvent")
		}
		if logItem.undoLine != line {
			t.Errorf("Event log list item should have the original line")
		}

		matches, _ := mockListRepo.Match([][]rune{}, nil, true)
		listItem := matches[0]
		if logItem.ptr != listItem {
			t.Errorf("The listItem ptr should be consistent with the original")
		}

		line2 := "Another item"
		mockListRepo.Add(line2, nil, listItem, nil)
		matches, _ = mockListRepo.Match([][]rune{}, nil, true)
		listItem2 := matches[1]

		if mockListRepo.eventLogDBHandler.log[1].ptr != logItem.ptr {
			t.Errorf("Original log item should still be in the first position in the log")
		}

		if len(mockListRepo.eventLogDBHandler.log) != 3 {
			t.Errorf("Event log should have one null and two real events in it")
		}
		if mockListRepo.eventLogDBHandler.curIdx != 2 {
			t.Errorf("Event logger should have incremented to two")
		}

		logItem2 := mockListRepo.eventLogDBHandler.log[2]
		if logItem2.eventType != addEvent {
			t.Errorf("Event log entry should be of type AddEvent")
		}
		if logItem2.undoLine != line2 {
			t.Errorf("Event log list item should have the new line")
		}

		if logItem2.ptr != listItem2 {
			t.Errorf("The listItem ptr should be consistent with the original")
		}

		mockListRepo.Delete(listItem2)

		if len(mockListRepo.eventLogDBHandler.log) != 4 {
			t.Errorf("Event log should have one null and three real events in it")
		}
		if mockListRepo.eventLogDBHandler.curIdx != 3 {
			t.Errorf("Event logger should have incremented to three")
		}

		logItem3 := mockListRepo.eventLogDBHandler.log[3]
		if logItem3.eventType != deleteEvent {
			t.Errorf("Event log entry should be of type DeleteEvent")
		}
		if logItem3.undoLine != line2 {
			t.Errorf("Event log list item should have the original line")
		}

		if logItem3.ptr != listItem2 {
			t.Errorf("The listItem ptr should be consistent with the original")
		}

		mockListRepo.Delete(matches[0])
		matches, _ = mockListRepo.Match([][]rune{}, nil, true)

		if len(mockListRepo.eventLogDBHandler.log) != 5 {
			t.Errorf("Event log should have one null and four real events in it")
		}
		if mockListRepo.eventLogDBHandler.curIdx != 4 {
			t.Errorf("Event logger should have incremented to four")
		}

		logItem4 := mockListRepo.eventLogDBHandler.log[4]
		if logItem4.eventType != deleteEvent {
			t.Errorf("Event log entry should be of type DeleteEvent")
		}
		if logItem4.undoLine != line {
			t.Errorf("Event log list item should have the original line")
		}

		if logItem4.ptr != listItem {
			t.Errorf("The listItem ptr should be consistent with the original")
		}

		err := mockListRepo.Undo()
		if err != nil {
			t.Fatal(err)
		}

		if len(mockListRepo.eventLogDBHandler.log) != 5 {
			t.Errorf("Event log should still have five events in it")
		}
		if mockListRepo.eventLogDBHandler.curIdx != 3 {
			t.Errorf("Event logger should have decremented to three")
		}

		matches, _ = mockListRepo.Match([][]rune{}, nil, true)
		if len(matches) != 1 {
			t.Errorf("Undo should have added the original item back in")
		}
		if matches[0].Line != line {
			t.Errorf("List item should now have the original line")
		}

		err = mockListRepo.Undo()
		if err != nil {
			t.Fatal(err)
		}

		if len(mockListRepo.eventLogDBHandler.log) != 5 {
			t.Errorf("Event log should still have five events in it")
		}
		if mockListRepo.eventLogDBHandler.curIdx != 2 {
			t.Errorf("Event logger should have decremented to two")
		}

		matches, _ = mockListRepo.Match([][]rune{}, nil, true)
		if len(matches) != 2 {
			t.Errorf("Undo should have added the second original item back in")
		}
		if matches[1].Line != line2 {
			t.Errorf("List item should now have the original line")
		}

		err = mockListRepo.Redo()
		if err != nil {
			t.Fatal(err)
		}

		if len(mockListRepo.eventLogDBHandler.log) != 5 {
			t.Errorf("Event log should still have five events in it")
		}
		if mockListRepo.eventLogDBHandler.curIdx != 3 {
			t.Errorf("Event logger should have incremented to three")
		}

		matches, _ = mockListRepo.Match([][]rune{}, nil, true)
		if len(matches) != 1 {
			t.Errorf("Undo should have removed the second original item again")
		}
		if matches[0].Line != line {
			t.Errorf("List item should now have the original line")
		}
	})
	t.Run("Add empty item, update with character, Undo, Redo", func(t *testing.T) {
		rootPath := "file_to_delete"
		mockListRepo := NewDBListRepo(rootPath, NewListItemDBHandler(""), NewEventLogDBHandler(""))
		defer os.Remove(rootPath)

		mockListRepo.Add("", nil, nil, nil)

		logItem := mockListRepo.eventLogDBHandler.log[1]
		if logItem.eventType != addEvent {
			t.Errorf("Event log entry should be of type AddEvent")
		}

		matches, _ := mockListRepo.Match([][]rune{}, nil, true)

		newLine := "a"
		mockListRepo.Update(newLine, []byte{}, matches[0])

		if len(mockListRepo.eventLogDBHandler.log) != 3 {
			t.Errorf("Event log should have one null and two real events in it")
		}
		if mockListRepo.eventLogDBHandler.curIdx != 2 {
			t.Errorf("Event logger should have incremented to two")
		}
		logItem2 := mockListRepo.eventLogDBHandler.log[2]
		if logItem2.eventType != updateEvent {
			t.Errorf("Event log entry should be of type UpdateEvent")
		}

		matches, _ = mockListRepo.Match([][]rune{}, nil, true)
		if matches[0].Line != newLine {
			t.Errorf("List item should now have the new line")
		}

		err := mockListRepo.Undo()
		if err != nil {
			t.Fatal(err)
		}

		if len(mockListRepo.eventLogDBHandler.log) != 3 {
			t.Errorf("Event log should still have three events in it")
		}
		if mockListRepo.eventLogDBHandler.curIdx != 1 {
			t.Errorf("Event logger should have decremented to one")
		}

		matches, _ = mockListRepo.Match([][]rune{}, nil, true)
		if matches[0].Line != "" {
			t.Errorf("Undo should have removed the line")
		}
		//fmt.Println(mockListRepo.eventLogDBHandler.curIdx)

		err = mockListRepo.Redo()
		if err != nil {
			t.Fatal(err)
		}

		if len(mockListRepo.eventLogDBHandler.log) != 3 {
			t.Errorf("Event log should still have two events in it")
		}
		if mockListRepo.eventLogDBHandler.curIdx != 2 {
			t.Errorf("Event logger should have returned to the head at two")
		}

		// TODO problem is, looking ahead to next log item for `Redo` redoes the old PRE state
		// Idea: store old and new state in the log item lines, Undo sets to old, Redo sets to new
		matches, _ = mockListRepo.Match([][]rune{}, nil, true)
		if matches[0].Line != newLine {
			t.Errorf("Redo should have added the line back in")
		}
	})
}
