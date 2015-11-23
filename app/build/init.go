// Copyright 2012 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build appengine

package build

import (
	"fmt"
	"net/http"
	"html/template"
	"encoding/json"
	"io/ioutil"

	"appengine"
	"appengine/datastore"
	"appengine/blobstore"

	"cache"
	"key"
)

func dashDebugHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-type", "text/plain; charset=utf-8")
	d := dashboardForRequest(r)
	c := d.Context(appengine.NewContext(r))
	defer cache.Tick(c)
	printDashboards(w, r, c, d)
}

func printDashboards(w http.ResponseWriter, r *http.Request, c appengine.Context, d *Dashboard) {
	fmt.Fprintf(w, "Dashboard's name: '%v'\n", d.Name)
	fmt.Fprintf(w, "Dashboard's namespace (empty means default): '%v'\n", d.Namespace)
	fmt.Fprintf(w, "Path prefix (no trailing /): '%v'\n", d.Prefix)
	fmt.Fprintf(w, "Package count: '%v'\n", len(d.Packages))

	for i, p := range d.Packages {
		fmt.Fprintf(w, "\nPackage %v: '%v'\n", i, p.Name)
		fmt.Fprintf(w, "\tKind: %v\n", p.Kind)
		fmt.Fprintf(w, "\tPath: %v\n", p.Path)
		fmt.Fprintf(w, "\tNextNum: %v\n", p.NextNum)
	}
}

func dashDebugJsonHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-type", "text/json; charset=utf-8")
	d := dashboardForRequest(r)
	c := d.Context(appengine.NewContext(r))
	defer cache.Tick(c)

	b, err := json.Marshal(d)
	if err != nil {
		logErr(w, r, err)
		return
	}

	w.Write(b)
}

func configFileHandler(w http.ResponseWriter, r *http.Request) {
	const rootTemplateHTML = `
<html><body>
<form action="{{.}}" method="POST" enctype="multipart/form-data">
Upload Config File: <input type="file" name="file"><br>
<input type="submit" name="submit" value="Submit">
</form></body></html>
`

	var rootTemplate = template.Must(template.New("root").Parse(rootTemplateHTML))

	c := appengine.NewContext(r)
	uploadURL, err := blobstore.UploadURL(c, "/upload", nil)
	if err != nil {
		logErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	err = rootTemplate.Execute(w, uploadURL)
	if err != nil {
		c.Errorf("%v", err)
	}
}

func configFileUploadHandler(w http.ResponseWriter, r *http.Request) {
        c := appengine.NewContext(r)
        blobs, _, err := blobstore.ParseUpload(r)
        if err != nil {
		logErr(w, r, err)
                return
        }
        file := blobs["file"]
        if len(file) == 0 {
                c.Errorf("no file uploaded")
                http.Redirect(w, r, "/", http.StatusFound)
                return
        }
        readConfigFileFromBlob(w, r, c, file[0])
}

type DashboardConfig struct {
	Response []Dashboard
	Error    string
}

func readConfigFileFromBlob(w http.ResponseWriter, r *http.Request, c appengine.Context, info *blobstore.BlobInfo) {
	blobReader := blobstore.NewReader(c, info.BlobKey)
	b, err := ioutil.ReadAll(blobReader);
	if err != nil {
                c.Errorf("error reading uploaded file: %v", err)
                http.Redirect(w, r, "/", http.StatusFound)
                return
	}
	var dashConf DashboardConfig
	if err := json.Unmarshal(b, &dashConf); err != nil {
                c.Errorf("error parsing json from uploaded file: %v", err)
                http.Redirect(w, r, "/", http.StatusFound)
                return
	}
	for _, d := range dashConf.Response {
		printDashboards(w, r, c, &d)
	}
}

func initHandler(w http.ResponseWriter, r *http.Request) {
	d := dashboardForRequest(r)
	c := d.Context(appengine.NewContext(r))
	defer cache.Tick(c)
	for _, p := range d.Packages {
		err := datastore.Get(c, p.Key(c), new(Package))
		if _, ok := err.(*datastore.ErrFieldMismatch); ok {
			// Some fields have been removed, so it's okay to ignore this error.
			err = nil
		}
		if err == nil {
			continue
		} else if err != datastore.ErrNoSuchEntity {
			logErr(w, r, err)
			return
		}
		p.NextNum = 1 // So we can add the first commit.
		if _, err := datastore.Put(c, p.Key(c), p); err != nil {
			logErr(w, r, err)
			return
		}
	}

	// Create secret key.
	key.Secret(c)

	// Create dummy config values.
	initConfig(c)

	// Populate Go 1.4 tag. This is for bootstrapping the new feature of
	// building sub-repos against the stable release.
	// TODO(adg): remove this after Go 1.5 is released, at which point the
	// build system will pick up on the new release tag automatically.
	t := &Tag{
		Kind: "release",
		Name: "release-branch.go1.4",
		Hash: "883bc6ed0ea815293fe6309d66f967ea60630e87", // Go 1.4.2
	}
	if _, err := datastore.Put(c, t.Key(c), t); err != nil {
		logErr(w, r, err)
		return
	}

	fmt.Fprint(w, "OK")
}
