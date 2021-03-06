// Copyright 2016 HenryLee. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package thinkgo

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/henrylee2cn/thinkgo/acceptencoder"
)

// Response wraps an http.ResponseWriter and implements its interface to be used
// by an HTTP handler to construct an HTTP response.
// See [http.ResponseWriter](https://golang.org/pkg/net/http/#ResponseWriter)
type Response struct {
	context   *Context
	writer    http.ResponseWriter
	status    int
	size      int64
	committed bool
}

var _ http.ResponseWriter = new(Response)

// newResponse creates a new instance of Response.
func newResponse(ctx *Context, w http.ResponseWriter) *Response {
	return &Response{
		context: ctx,
		writer:  w,
	}
}

func (resp *Response) reset(w http.ResponseWriter) {
	resp.writer = w
	resp.status = 0
	resp.size = 0
	resp.committed = false
}

// Header returns the header map that will be sent by
// WriteHeader. Changing the header after a call to
// WriteHeader (or Write) has no effect unless the modified
// headers were declared as trailers by setting the
// "Trailer" header before the call to WriteHeader (see example).
// To suppress implicit response headers, set their value to nil.
func (resp *Response) Header() http.Header {
	return resp.writer.Header()
}

// WriteHeader sends an HTTP response header with status code.
// If WriteHeader is not called explicitly, the first call to Write
// will trigger an implicit WriteHeader(http.StatusOK).
// Thus explicit calls to WriteHeader are mainly used to
// send error codes.
func (resp *Response) WriteHeader(status int) {
	if resp.committed {
		resp.context.Log().Warning("multiple response.WriteHeader calls")
		return
	}
	resp.status = status
	resp.context.beforeWriteHeader()
	resp.writer.WriteHeader(status)
	resp.committed = true
}

// Write writes the data to the connection as part of an HTTP reply.
// If WriteHeader has not yet been called, Write calls WriteHeader(http.StatusOK)
// before writing the data.  If the Header does not contain a
// Content-Type line, Write adds a Content-Type set to the result of passing
// the initial 512 bytes of written data to DetectContentType.
func (resp *Response) Write(b []byte) (int, error) {
	n, err := resp.writer.Write(b)
	resp.size += int64(n)
	return n, err
}

// AddCookie adds a Set-Cookie header.
// The provided cookie must have a valid Name. Invalid cookies may be
// silently dropped.
func (r *Response) AddCookie(cookie *http.Cookie) {
	r.Header().Add(HeaderSetCookie, cookie.String())
}

// SetCookie sets a Set-Cookie header.
func (r *Response) SetCookie(cookie *http.Cookie) {
	r.Header().Set(HeaderSetCookie, cookie.String())
}

// DelCookie sets Set-Cookie header.
func (r *Response) DelCookie() {
	r.Header().Del(HeaderSetCookie)
}

// Flush implements the http.Flusher interface to allow an HTTP handler to flush
// buffered data to the client.
func (resp *Response) Flush() {
	if f, ok := resp.writer.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements the http.Hijacker interface to allow an HTTP handler to
// take over the connection.
func (resp *Response) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := resp.writer.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("webserver doesn't support hijacking")
	}
	return hj.Hijack()
}

// CloseNotify implements the http.CloseNotifier interface to allow detecting
// when the underlying connection has gone away.
// This mechanism can be used to cancel long operations on the server if the
// client has disconnected before the response is ready.
func (resp *Response) CloseNotify() <-chan bool {
	if cn, ok := resp.writer.(http.CloseNotifier); ok {
		return cn.CloseNotify()
	}
	return nil
}

// Status returns the HTTP status code of the response.
func (resp *Response) Status() int {
	return resp.status
}

// Size returns the current size, in bytes, of the response.
func (resp *Response) Size() int64 {
	return resp.size
}

// Committed asserts whether or not the response has been committed to.
func (resp *Response) Committed() bool {
	return resp.committed
}

// SetHeader sets response header item string via given key.
func (ctx *Context) SetHeader(key, val string) {
	ctx.W.Header().Set(key, val)
}

// SetContentType sets the content type from ext string.
// MIME type is given in mime package.
func (ctx *Context) SetContentType(ext string) {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	ctype := mime.TypeByExtension(ext)
	if ctype != "" {
		ctx.W.Header().Set(HeaderContentType, ctype)
	}
}

// IsCachable returns boolean of this request is cached.
// HTTP 304 means cached.
func (ctx *Context) IsCachable() bool {
	return ctx.W.status >= 200 && ctx.W.status < 300 || ctx.W.status == 304
}

// IsEmpty returns boolean of this request is empty.
// HTTP 201，204 and 304 means empty.
func (ctx *Context) IsEmpty() bool {
	return ctx.W.status == 201 || ctx.W.status == 204 || ctx.W.status == 304
}

// IsOk returns boolean of this request runs well.
// HTTP 200 means ok.
func (ctx *Context) IsOk() bool {
	return ctx.W.status == 200
}

// IsSuccessful returns boolean of this request runs successfully.
// HTTP 2xx means ok.
func (ctx *Context) IsSuccessful() bool {
	return ctx.W.status >= 200 && ctx.W.status < 300
}

// IsRedirect returns boolean of this request is redirection header.
// HTTP 301,302,307 means redirection.
func (ctx *Context) IsRedirect() bool {
	return ctx.W.status == 301 || ctx.W.status == 302 || ctx.W.status == 303 || ctx.W.status == 307
}

// IsForbidden returns boolean of this request is forbidden.
// HTTP 403 means forbidden.
func (ctx *Context) IsForbidden() bool {
	return ctx.W.status == 403
}

// IsNotFound returns boolean of this request is not found.
// HTTP 404 means forbidden.
func (ctx *Context) IsNotFound() bool {
	return ctx.W.status == 404
}

// IsClientError returns boolean of this request client sends error data.
// HTTP 4xx means forbidden.
func (ctx *Context) IsClientError() bool {
	return ctx.W.status >= 400 && ctx.W.status < 500
}

// IsServerError returns boolean of this server handler errors.
// HTTP 5xx means server internal error.
func (ctx *Context) IsServerError() bool {
	return ctx.W.status >= 500 && ctx.W.status < 600
}

// SetCookie sets cookie value via given key.
// others are ordered as cookie's max age time, path, domain, secure and httponly.
func (ctx *Context) SetCookie(name string, value string, others ...interface{}) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s=%s", sanitizeName(name), sanitizeValue(value))
	//fix cookie not work in IE
	if len(others) > 0 {
		var maxAge int64
		switch v := others[0].(type) {
		case int:
			maxAge = int64(v)
		case int32:
			maxAge = int64(v)
		case int64:
			maxAge = v
		}
		switch {
		case maxAge > 0:
			fmt.Fprintf(&b, "; Expires=%s; Max-Age=%d", time.Now().Add(time.Duration(maxAge)*time.Second).UTC().Format(time.RFC1123), maxAge)
		case maxAge < 0:
			fmt.Fprintf(&b, "; Max-Age=0")
		}
	}
	// the settings below
	// Path, Domain, Secure, HttpOnly
	// can use nil skip set

	// default "/"
	if len(others) > 1 {
		if v, ok := others[1].(string); ok && len(v) > 0 {
			fmt.Fprintf(&b, "; Path=%s", sanitizeValue(v))
		}
	} else {
		fmt.Fprintf(&b, "; Path=%s", "/")
	}

	// default empty
	if len(others) > 2 {
		if v, ok := others[2].(string); ok && len(v) > 0 {
			fmt.Fprintf(&b, "; Domain=%s", sanitizeValue(v))
		}
	}

	// default empty
	if len(others) > 3 {
		var secure bool
		switch v := others[3].(type) {
		case bool:
			secure = v
		default:
			if others[3] != nil {
				secure = true
			}
		}
		if secure {
			fmt.Fprintf(&b, "; Secure")
		}
	}

	// default false. for session cookie default true
	httponly := false
	if len(others) > 4 {
		if v, ok := others[4].(bool); ok && v {
			// HttpOnly = true
			httponly = true
		}
	}

	if httponly {
		fmt.Fprintf(&b, "; HttpOnly")
	}

	ctx.W.Header().Add(HeaderSetCookie, b.String())
}

var cookieNameSanitizer = strings.NewReplacer("\n", "-", "\r", "-")

func sanitizeName(n string) string {
	return cookieNameSanitizer.Replace(n)
}

var cookieValueSanitizer = strings.NewReplacer("\n", " ", "\r", " ", ";", " ")

func sanitizeValue(v string) string {
	return cookieValueSanitizer.Replace(v)
}

// SetSecureCookie Set Secure cookie for response.
func (ctx *Context) SetSecureCookie(secret, name, value string, others ...interface{}) {
	vs := base64.URLEncoding.EncodeToString([]byte(value))
	timestamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	h := hmac.New(sha1.New, []byte(secret))
	fmt.Fprintf(h, "%s%s", vs, timestamp)
	sig := fmt.Sprintf("%02x", h.Sum(nil))
	cookie := strings.Join([]string{vs, timestamp, sig}, "|")
	ctx.SetCookie(name, cookie, others...)
}

// NoContent sends a response with no body and a status code.
func (ctx *Context) NoContent(status int) {
	ctx.W.WriteHeader(status)
}

// Send error message and stop handler chain.
func (ctx *Context) Error(status int, errStr string) {
	ctx.frame.errorFunc(ctx, errStr, status)
	ctx.Stop()
}

// Send writes the data to the connection as part of an HTTP reply.
func (ctx *Context) Send(status int, content []byte) error {
	if ctx.W.committed {
		return errors.New("multiple response.WriteHeader calls")
	}
	if ctx.W.Header().Get(HeaderContentEncoding) == "" {
		if ctx.enableGzip {
			encoding := acceptencoder.ParseEncoding(ctx.R)
			buf := &bytes.Buffer{}
			if b, n, _ := acceptencoder.WriteBody(encoding, buf, content); b {
				ctx.W.Header().Set(HeaderContentEncoding, n)
				ctx.W.WriteHeader(status)
				_, err := io.Copy(ctx.W, buf)
				return err
			}
		}
		if ctx.W.Header().Get(HeaderContentLength) == "" {
			ctx.W.Header().Set(HeaderContentLength, strconv.Itoa(len(content)))
		}
	}
	ctx.W.WriteHeader(status)
	_, err := ctx.W.Write(content)
	return err
}

// String writes a string to the client, something like fmt.Fprintf
func (ctx *Context) String(status int, format string, s ...interface{}) error {
	ctx.W.Header().Set(HeaderContentType, MIMETextPlainCharsetUTF8)
	if len(s) == 0 {
		return ctx.Send(status, []byte(format))
	}
	return ctx.Send(status, []byte(fmt.Sprintf(format, s...)))
}

// HTML sends an HTTP response with status code.
func (ctx *Context) HTML(status int, html string) error {
	x := (*[2]uintptr)(unsafe.Pointer(&html))
	h := [3]uintptr{x[0], x[1], x[1]}
	ctx.W.Header().Set(HeaderContentType, MIMETextHTMLCharsetUTF8)
	return ctx.Send(status, *(*[]byte)(unsafe.Pointer(&h)))
}

// JSON sends a JSON response with status code.
func (ctx *Context) JSON(status int, data interface{}) error {
	var (
		b   []byte
		err error
	)
	if ctx.frame.config.RunMode == RUNMODE_PROD {
		b, err = json.Marshal(data)
	} else {
		b, err = json.MarshalIndent(data, "", "  ")
	}
	if err != nil {
		return err
	}
	return ctx.JSONBlob(status, b)
}

// JSONBlob sends a JSON blob response with status code.
func (ctx *Context) JSONBlob(status int, b []byte) error {
	ctx.W.Header().Set(HeaderContentType, MIMEApplicationJSONCharsetUTF8)
	return ctx.Send(status, b)
}

// JSONP sends a JSONP response with status code. It uses `callback` to construct
// the JSONP payload.
func (ctx *Context) JSONP(status int, callback string, data interface{}) error {
	var (
		b   []byte
		err error
	)
	if ctx.frame.config.RunMode == RUNMODE_PROD {
		b, err = json.Marshal(data)
	} else {
		b, err = json.MarshalIndent(data, "", "  ")
	}
	if err != nil {
		return err
	}
	ctx.W.Header().Set(HeaderContentType, MIMEApplicationJavaScriptCharsetUTF8)
	callback = template.JSEscapeString(callback)
	callbackContent := bytes.NewBufferString(" if(window." + callback + ")" + callback)
	callbackContent.WriteString("(")
	callbackContent.Write(b)
	callbackContent.WriteString(");\r\n")
	return ctx.Send(status, callbackContent.Bytes())
}

// XML sends an XML response with status code.
func (ctx *Context) XML(status int, data interface{}) error {
	var (
		b   []byte
		err error
	)
	if ctx.frame.config.RunMode == RUNMODE_PROD {
		b, err = xml.Marshal(data)
	} else {
		b, err = xml.MarshalIndent(data, "", "  ")
	}
	if err != nil {
		return err
	}
	return ctx.XMLBlob(status, b)
}

// XMLBlob sends a XML blob response with status code.
func (ctx *Context) XMLBlob(status int, b []byte) error {
	ctx.W.Header().Set(HeaderContentType, MIMEApplicationXMLCharsetUTF8)
	content := bytes.NewBufferString(xml.Header)
	content.Write(b)
	return ctx.Send(status, content.Bytes())
}

// JSONOrXML serve Xml OR Json, depending on the value of the Accept header
func (ctx *Context) JSONOrXML(status int, data interface{}) error {
	if ctx.AcceptJSON() || !ctx.AcceptXML() {
		return ctx.JSON(status, data)
	}
	return ctx.XML(status, data)
}

// File forces response for download file.
// it prepares the download response header automatically.
func (ctx *Context) File(file string, filename ...string) {
	ctx.W.Header().Set(HeaderContentDescription, "File Transfer")
	ctx.W.Header().Set(HeaderContentType, MIMEOctetStream)
	if len(filename) > 0 && filename[0] != "" {
		ctx.W.Header().Set(HeaderContentDisposition, "attachment; filename="+filename[0])
	} else {
		ctx.W.Header().Set(HeaderContentDisposition, "attachment; filename="+filepath.Base(file))
	}
	ctx.W.Header().Set(HeaderContentTransferEncoding, "binary")
	ctx.W.Header().Set(HeaderExpires, "0")
	ctx.W.Header().Set(HeaderCacheControl, "must-revalidate")
	ctx.W.Header().Set(HeaderPragma, "public")
	ctx.frame.fileServerManager.ServeFile(ctx, file)
}
