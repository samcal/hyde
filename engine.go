package hyde

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/elos/autonomous"
	"github.com/go-fsnotify/fsnotify"
)

type Engine struct {
	autonomous.Life
	autonomous.Managed
	autonomous.Stopper

	w        *fsnotify.Watcher
	RootedAt string
	fmap     *FileMap

	NodeChanges chan *FileNode
	NodeRemoves chan *FileNode
}

func (e *Engine) FileMap() FileMap {
	return *e.fmap
}

func NewEngine(atPath string) (*Engine, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	fm := make(FileMap)

	e := &Engine{
		fmap:     &fm,
		w:        watcher,
		RootedAt: atPath,

		Life:        autonomous.NewLife(),
		Stopper:     make(autonomous.Stopper),
		NodeChanges: make(chan *FileNode, 20),
	}

	e.load(e.RootedAt)

	return e, nil
}

func (e *Engine) watch(path string) {
	e.w.Add(path)
}

func (e *Engine) load(path string) {
	file, err := os.Stat(path)
	if err != nil {
		return
	}

	base := filepath.Base(path)
	if len(base) > 0 && string(base[0]) == "." {
		return // cause file is hidden
	}

	if file.IsDir() {
		e.watch(path)
		files, err := ioutil.ReadDir(path)
		if err == nil {
			for _, f := range files {
				e.load(filepath.Join(path, f.Name()))
			}
		}
	}

	node := NewFileNode(path, e.RootedAt)
	(*e.fmap)[path] = node
	go func() { e.NodeChanges <- node }()
}

func (e *Engine) remove(path string) {
	node, ok := (*e.fmap)[path]

	if ok {
		go func() { e.NodeRemoves <- node }()
		delete(*e.fmap, path)
	}
}

func (e *Engine) Start() {
	e.Life.Begin()

	events := make(chan *fsnotify.Event)
	errors := make(chan error)

	go func() {
		for {
			select {
			case event := <-e.w.Events:
				events <- &event
			case err := <-e.w.Errors:
				errors <- err
			}
		}
	}()

Run:
	for {
		select {
		case event := <-events:
			e.process(event)
		case err := <-errors:
			log.Printf("watcher error:", err)
			go e.Stop()
		case <-e.Stopper:
			break Run
		}
	}

	e.w.Close()
	e.Life.End()
}

func (e *Engine) process(event *fsnotify.Event) {
	log.Print(event)
	switch event.Op {
	case fsnotify.Create:
		e.load(event.Name)
	case fsnotify.Write:
		e.load(event.Name)
	case fsnotify.Remove:
		e.remove(event.Name)
	case fsnotify.Rename:
		e.remove(event.Name)
	}
}
