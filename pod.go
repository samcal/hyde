package hyde

import (
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/elos/autonomous"
	"github.com/julienschmidt/httprouter"
	"github.com/russross/blackfriday"
)

type LayoutDeck map[string]*template.Template

func (ld LayoutDeck) Add(extension, location string) {
	t, _ := template.ParseFiles(location)
	ld[extension] = t
}

type Pod struct {
	autonomous.Life
	autonomous.Managed
	autonomous.Stopper

	*Engine
	registered map[string]httprouter.Handle
	layout     *template.Template
	layoutDeck LayoutDeck
}

func NewPod(e *Engine, layout *template.Template, ld LayoutDeck) *Pod {
	p := &Pod{
		Life:    autonomous.NewLife(),
		Stopper: make(autonomous.Stopper),

		Engine:     e,
		registered: make(map[string]httprouter.Handle),

		layout:     layout,
		layoutDeck: ld,
	}

	return p
}

func (p *Pod) Name() string {
	return filepath.Base(p.RootedAt)
}

func (p *Pod) Start() {
	p.Life.Begin()

Run:
	for {
		select {
		case fn := <-p.NodeChanges:
			p.mount(fn)
		case fn := <-p.NodeRemoves:
			p.dismount(fn)
		case <-p.Stopper:
			break Run
		}
	}

	p.Life.End()
}

func (p *Pod) route(fn *FileNode) string {
	return filepath.Join("/", p.Name(), fn.RelPath())
}

func (p *Pod) mount(fn *FileNode) {
	route := p.route(fn)
	p.registered[route] = p.handle(fn)
}

func (p *Pod) dismount(fn *FileNode) {
	delete(p.registered, p.route(fn))
}

var extensionLanguages = map[string]string{
	".sh": "bash",
	".go": "go",
}

func codeFile(input []byte, ext, name string) template.HTML {
	return template.HTML(fmt.Sprintf("<h2>%s</h2><pre><code class='language-%s'>%s</code></pre>",
		name, extensionLanguages[ext], template.HTML(input)))
}

func (p *Pod) handle(fn *FileNode) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
		input, err := ioutil.ReadFile(fn.Path)
		if err != nil {
			if strings.Contains(err.Error(), "is a directory") {
				input, err = ioutil.ReadFile(filepath.Join(fn.Path, "README.md"))
				if err != nil {
					input = []byte(fmt.Sprintf("This is the %s directory, you can add a README.md, which will show up here", p.route(fn)))
				}
			} else {
				log.Print(err)
				return
			}
		}

		extension := filepath.Ext(fn.Path)

		var content template.HTML

		if extension == ".md" {
			output := blackfriday.MarkdownCommon(input)
			content = template.HTML(output)
		} else if _, ok := extensionLanguages[extension]; ok {
			content = codeFile(input, extension, fn.Name())
		} else if extension == ".png" {
			w.Write(input)
			return
		} else if extension == "" {
			content = template.HTML(input)
		}

		p.layout.Execute(w, Page{
			Nav:     p.treeNav(),
			Content: content,
		})
	}

}

type TreeNav struct {
	Name, Link string
	*FileNode
	Subs map[string]TreeNav
}

type Page struct {
	Nav     TreeNav
	Content template.HTML
}

func (p *Pod) treeNav() TreeNav {
	t := TreeNav{
		Name: strings.ToUpper(p.Name()),
		Link: filepath.Join("/", p.Name()),
		Subs: make(map[string]TreeNav),
	}

	for route, _ := range p.registered {
		splits := strings.Split(route, "/")
		tt := t

		subpath := "/"
		for _, level := range splits {
			subpath = filepath.Join(subpath, level)
			_, ok := tt.Subs[level]

			if ok {
				tt = tt.Subs[level]
				continue
			}

			nt := TreeNav{
				Name: level,
				Link: subpath,
				Subs: make(map[string]TreeNav),
			}

			tt.Subs[level] = nt
			tt = nt
		}
	}

	return t
}

func (p *Pod) Route(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	route := filepath.Join("/", p.Name(), params.ByName("subpath"))
	if handle, ok := p.registered[route]; ok {
		handle(w, r, params)
	} else {
		http.NotFound(w, r)
	}
}
