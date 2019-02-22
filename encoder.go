package main

import (
	"bytes"
	"github.com/CatalystG/protoc-gen-gotemplate/proto"
	"github.com/golang/protobuf/proto"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/golang/protobuf/protoc-gen-go/plugin"

	"moul.io/protoc-gen-gotemplate/helpers"
)

type GenericTemplateBasedEncoder struct {
	templateDir    string
	service        *descriptor.ServiceDescriptorProto
	file           *descriptor.FileDescriptorProto
	enum           []*descriptor.EnumDescriptorProto
	debug          bool
	destinationDir string
}

type Ast struct {
	BuildDate      time.Time                          `json:"build-date"`
	BuildHostname  string                             `json:"build-hostname"`
	BuildUser      string                             `json:"build-user"`
	GoPWD          string                             `json:"go-pwd,omitempty"`
	PWD            string                             `json:"pwd"`
	Debug          bool                               `json:"debug"`
	DestinationDir string                             `json:"destination-dir"`
	File           *descriptor.FileDescriptorProto    `json:"file"`
	RawFilename    string                             `json:"raw-filename"`
	Filename       string                             `json:"filename"`
	TemplateDir    string                             `json:"template-dir"`
	Service        *descriptor.ServiceDescriptorProto `json:"service"`
	Enum           []*descriptor.EnumDescriptorProto  `json:"enum"`
	Options        map[string]*gotemplate.GoTemplateOption       `json:"options"`
	Path		   string `json:"path"`
}

func NewGenericServiceTemplateBasedEncoder(templateDir string, service *descriptor.ServiceDescriptorProto, file *descriptor.FileDescriptorProto, debug bool, destinationDir string) (e *GenericTemplateBasedEncoder) {
	e = &GenericTemplateBasedEncoder{
		service:        service,
		file:           file,
		templateDir:    templateDir,
		debug:          debug,
		destinationDir: destinationDir,
		enum:           file.GetEnumType(),
	}
	if debug {
		log.Printf("new encoder: file=%q service=%q template-dir=%q", file.GetName(), service.GetName(), templateDir)
	}
	pgghelpers.InitPathMap(file)

	return
}

func NewGenericTemplateBasedEncoder(templateDir string, file *descriptor.FileDescriptorProto, debug bool, destinationDir string) (e *GenericTemplateBasedEncoder) {
	e = &GenericTemplateBasedEncoder{
		service:        nil,
		file:           file,
		templateDir:    templateDir,
		enum:           file.GetEnumType(),
		debug:          debug,
		destinationDir: destinationDir,
	}
	if debug {
		log.Printf("new encoder: file=%q template-dir=%q", file.GetName(), templateDir)
	}
	pgghelpers.InitPathMap(file)

	return
}

func (e *GenericTemplateBasedEncoder) templates() ([]string, error) {
	filenames := []string{}

	err := filepath.Walk(e.templateDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".tmpl" {
			return nil
		}
		rel, err := filepath.Rel(e.templateDir, path)
		if err != nil {
			return err
		}
		if e.debug {
			log.Printf("new template: %q", rel)
		}

		filenames = append(filenames, rel)
		return nil
	})
	return filenames, err
}

func (e *GenericTemplateBasedEncoder) genAst(templateFilename string) (*Ast, error) {
	// prepare the ast passed to the template engine
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	pwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	goPwd := ""
	if os.Getenv("GOPATH") != "" {
		goPwd, err = filepath.Rel(os.Getenv("GOPATH")+"/src", pwd)
		if err != nil {
			return nil, err
		}
		if strings.Contains(goPwd, "../") {
			goPwd = ""
		}
	}

	// Get the options: this should be a new function helper probably instead.
	gto := make(map[string]*gotemplate.GoTemplateOption)
	for _, v := range e.file.MessageType {
		if v == nil || v.Options == nil {
			continue
		}
		mo := v.Options
		i, _ := proto.GetExtension(mo, gotemplate.E_GoTemplateOption)
		if i != nil {
			gto[*v.Name] = i.(*gotemplate.GoTemplateOption)
		}
	}

	ast := Ast{
		BuildDate:      time.Now(),
		BuildHostname:  hostname,
		BuildUser:      os.Getenv("USER"),
		PWD:            pwd,
		GoPWD:          goPwd,
		File:           e.file,
		TemplateDir:    e.templateDir,
		DestinationDir: e.destinationDir,
		RawFilename:    templateFilename,
		Filename:       "",
		Service:        e.service,
		Enum:           e.enum,
		Options:        gto,
		Path: filepath.Dir(*e.file.Name),
	}
	buffer := new(bytes.Buffer)

	unescaped, err := url.QueryUnescape(templateFilename)
	if err != nil {
		log.Printf("failed to unescape filepath %q: %v", templateFilename, err)
	} else {
		templateFilename = unescaped
	}

	tmpl, err := template.New("").Funcs(pgghelpers.ProtoHelpersFuncMap).Parse(templateFilename)
	if err != nil {
		return nil, err
	}
	if err := tmpl.Execute(buffer, ast); err != nil {
		return nil, err
	}
	ast.Filename = buffer.String()
	return &ast, nil
}

func (e *GenericTemplateBasedEncoder) buildContent(templateFilename string) (string, string, error) {
	// initialize template engine
	fullPath := filepath.Join(e.templateDir, templateFilename)
	templateName := filepath.Base(fullPath)
	tmpl, err := template.New(templateName).Funcs(pgghelpers.ProtoHelpersFuncMap).ParseFiles(fullPath)
	if err != nil {
		return "", "", err
	}

	ast, err := e.genAst(templateFilename)
	if err != nil {
		return "", "", err
	}

	// generate the content
	buffer := new(bytes.Buffer)
	if err := tmpl.Execute(buffer, ast); err != nil {
		return "", "", err
	}

	s := filepath.Dir(ast.Filename) + "/" + filepath.Base(*(ast.File.Name)) + "_" + filepath.Base(ast.Filename)
	return buffer.String(), s, nil
}

func (e *GenericTemplateBasedEncoder) Files() []*plugin_go.CodeGeneratorResponse_File {
	templates, err := e.templates()
	if err != nil {
		log.Fatalf("cannot get templates from %q: %v", e.templateDir, err)
	}

	length := len(templates)
	files := make([]*plugin_go.CodeGeneratorResponse_File, 0, length)
	errChan := make(chan error, length)
	ignoreChan := make(chan string, length)
	resultChan := make(chan *plugin_go.CodeGeneratorResponse_File, length)
	for _, templateFilename := range templates {
		go func(tmpl string) {
			var translatedFilename, content string
			content, translatedFilename, err = e.buildContent(tmpl)
			if err != nil {
				errChan <- err
				return
			}
			filename := translatedFilename[:len(translatedFilename)-len(".tmpl")]

			if content == "" {
				ignoreChan <- ""
				return
			}

			resultChan <- &plugin_go.CodeGeneratorResponse_File{
				Content: &content,
				Name:    &filename,
			}
		}(templateFilename)
	}
	for i := 0; i < length; i++ {
		select {
		case <-ignoreChan:
		case f := <-resultChan:
			files = append(files, f)
		case err = <-errChan:
			panic(err)
		}
	}
	return files
}
