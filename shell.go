package main

/*
   Copyright (C) 2014,2015 Kouhei Maeda <mkouhei@palmtb.net>

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU General Public License for more details.

   You should have received a copy of the GNU General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

import (
	"bufio"
	"fmt"
	"os"
)

func (e *env) read(fp *os.File, wrCh, quitCh chan<- bool, imptQ chan<- imptSpec) {
	// read from shell prompt
	go func() {
		reader := bufio.NewReader(fp)
		for {
			if e.readFlag == 0 {
				fmt.Print(">>> ")
			} else {
				e.readFlag--
			}
			line, _, err := reader.ReadLine()
			if err != nil {
				e.logger("read", "", err)
				cleanDir(e.bldDir)
				quitCh <- true
				return
			}

			// append token.SEMICOLON
			line = append(line, 59)

			if e.parserSrc.parseLine(line, imptQ) {
				wrCh <- true
				e.readFlag = 3
			}
		}
	}()
}

func (e *env) write(imptCh chan<- bool) {
	// write tmporary source code file
	go func() {
		f, err := os.OpenFile(e.tmpPath, os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			return
		}
		f.Truncate(0)

		f.WriteString(concatLines(e.parserSrc.mergeLines(), "\n"))
		e.logger("write", concatLines(e.parserSrc.mergeLines(), "\n"), nil)
		f.Sync()
		if err := f.Close(); err != nil {
			e.logger("writer", "", err)
			return
		}

		imptCh <- true
		e.parserSrc.main = nil
		removePrintStmt(&e.parserSrc.mainHist)
	}()
}

func (e *env) goRun() {
	// execute `go run'
	os.Chdir(e.bldDir)
	var cmd string
	var args []string

	if e.sudo == "" {
		cmd = "go"
		args = []string{"run", tmpname}
	} else {
		cmd = "sudo"
		args = []string{"-E", "-S", "-p", "''", "go", "run", tmpname}
	}

	omitFlag := false
	if len(e.parserSrc.mainHist) > 0 {
		omitFlag = true
	}

	if msg, err := e.runCmd(true, omitFlag, cmd, args...); err != nil {
		e.logger("go run", msg, err)
		e.parserSrc.body = nil
		return
	}
}

func pkgName(name, path string) string {
	s := ""
	if name == "" {
		s = path
	} else {
		s = name
	}
	return s
}

func (e *env) goGet(imptQ <-chan imptSpec) {
	// execute `go get'
	go func() {
		for {
			pkg := <-imptQ
			args := []string{"get", pkg.imPath}
			if msg, err := e.runCmd(true, false, "go", args...); err != nil {
				e.parserSrc.imPkgs.removeImport(msg, pkg)
				e.logger("go get", msg, err)
			}
		}
	}()
}

func (e *env) goImports(execCh chan<- bool) {
	// execute `goimports'
	go func() {
		args := []string{"-w", e.tmpPath}
		if msg, err := e.runCmd(true, false, "goimports", args...); err != nil {
			e.logger("goimports", msg, err)
			e.parserSrc.body = nil
		}
		execCh <- true

	}()
}

func (e *env) shell(fp *os.File) {
	// main shell loop

	if fp == nil {
		fp = os.Stdin
	}

	// quit channel
	quitCh := make(chan bool)
	// write channel
	wrCh := make(chan bool)
	// import channel
	imptCh := make(chan bool)
	// execute channel
	execCh := make(chan bool)
	// package queue for go get
	imptQ := make(chan imptSpec, 10)

	e.goGet(imptQ)

loop:
	for {
		e.read(fp, wrCh, quitCh, imptQ)

		select {
		case <-wrCh:
			e.write(imptCh)
		case <-imptCh:
			e.goImports(execCh)
		case <-execCh:
			e.goRun()
		case <-quitCh:
			cleanDir(e.bldDir)
			fmt.Println("[gosh] terminated")
			break loop
		}
	}

	return
}
