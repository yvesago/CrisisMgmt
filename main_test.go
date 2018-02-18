package main

import (
	//"flag"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"
)

func init() {
	rand.Seed(1) // fix rand fo tests
}

func TestParse(t *testing.T) {

	assert.Equal(t, "XpdUC2sK8Tpb", randStringBytes(12), "test 12 char rand string")

	// start
	o := []byte(`{"CMD":"init"}`)

	emsg, hcmd, pass, file, pre := ParseEntry(o)

	assert.Equal(t, "init", hcmd, "CMD start")
	assert.Equal(t, "", emsg, "no error")
	assert.Equal(t, "", pass, "pass")
	assert.Equal(t, "", file, "file")
	assert.Equal(t, "", pre, "prefix")

	// start old session
	o = []byte(`{"CMD":"start","Pass":"qwerty1Q","File":"a12-02-2018","Pre":""}`)
	emsg, hcmd, pass, file, pre = ParseEntry(o)
	assert.Equal(t, "start", hcmd, "CMD start")
	assert.Equal(t, "", emsg, "no error")
	assert.Equal(t, "qwerty1Q", pass, "pass")
	assert.Equal(t, "a12-02-2018", file, "file")
	assert.Equal(t, "", pre, "prefix")

	// create new session
	o = []byte(`{"CMD":"start","Pass":"qwerty1Q","File":"","Pre":"a"}`)
	emsg, hcmd, pass, file, pre = ParseEntry(o)
	assert.Equal(t, "start", hcmd, "CMD start")
	assert.Equal(t, "", emsg, "no error")
	assert.Equal(t, "qwerty1Q", pass, "pass")
	assert.Equal(t, "", file, "file")
	assert.Equal(t, "a", pre, "prefix")

	// test bad CMD
	o = []byte(`{"CMD":"xxx","Pass":"qwerty1Q","File":"a12-02-2018","Pre":"a"}`)
	emsg, hcmd, pass, file, pre = ParseEntry(o)

	assert.Equal(t, "error", hcmd, "CMD error")
	assert.Equal(t, "Bad cmd", emsg, "no error")
	assert.Equal(t, "", pre, "prefix")

	// test bad pass
	o = []byte(`{"CMD":"start","Pass":"qwerty","File":"a12-02-2018","Pre":"a"}`)
	emsg, hcmd, pass, file, pre = ParseEntry(o)

	assert.Equal(t, "error", hcmd, "CMD error")
	assert.Equal(t, "Bad password", emsg, "no error")
	assert.Equal(t, "", pass, "bad password")
	assert.Equal(t, "", pre, "prefix")

	// test bad prefix
	o = []byte(`{"CMD":"start","Pass":"qwerty1Q","File":"","Pre":"..2\n"}`)
	emsg, hcmd, pass, file, pre = ParseEntry(o)

	assert.Equal(t, "error", hcmd, "CMD error")
	assert.Equal(t, "Bad prefix", emsg, "prefix error")
	assert.Equal(t, "", pre, "bad prefix")

	o = []byte(`{"CMD":"start","Pass":"qwerty1Q","File":"../a12-02-2018","Pre":"a"}`)
	emsg, hcmd, pass, file, pre = ParseEntry(o)

	assert.Equal(t, "error", hcmd, "CMD error")
	assert.Equal(t, "Bad file name", emsg, "no error")
	assert.Equal(t, "", pre, "no error")
	assert.Equal(t, "", file, "bad file name")
	assert.Equal(t, "qwerty1Q", pass, "no error")

	sevenOrMore, number, upper, special := verifyPassword("qwerty1Q@")
	assert.Equal(t, true, sevenOrMore, "sevenOrMore")
	assert.Equal(t, true, number, "number")
	assert.Equal(t, true, upper, "upper")
	assert.Equal(t, true, special, "special")
	validPass := false
	validPass = sevenOrMore && number && upper && special
	assert.Equal(t, true, validPass, "valid pass")

	sevenOrMore, number, upper, special = verifyPassword("qwerty1Q")
	assert.Equal(t, true, sevenOrMore, "sevenOrMore")
	assert.Equal(t, true, number, "number")
	assert.Equal(t, true, upper, "upper")
	assert.Equal(t, false, special, "special")
	validPass = false
	validPass = sevenOrMore && number && upper && special
	assert.Equal(t, false, validPass, "unvalid pass")

	sevenOrMore, number, upper, special = verifyPassword("qwerty")
	assert.Equal(t, false, sevenOrMore, "sevenOrMore")
	assert.Equal(t, false, number, "no number")
	assert.Equal(t, false, upper, "no upper case")
	assert.Equal(t, false, special, "no special")

}

func TestServer(t *testing.T) {

	var config = Config{
		User:       "crise",
		Server:     "http://exemple.com",
		CorsOrigin: "*",
		PublicDir:  "dir",
		PublicPath: "dir/",
		MngmtDir:   "",
		AuthAdmins: []string{"", "admin"}, // allow access whithout auth
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()

	server(router, true, config)

	/**
	test access
	**/

	req, err := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-User", "user")
	resp1 := httptest.NewRecorder()
	router.ServeHTTP(resp1, req)
	assert.Equal(t, 401, resp1.Code, "access denied")

	req, err = http.NewRequest("GET", "/", nil)
	resp1 = httptest.NewRecorder()
	router.ServeHTTP(resp1, req)
	assert.Equal(t, 200, resp1.Code, "access allow whithout auth")

	/**
	test template
	**/

	req, err = http.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-User", "admin")
	if err != nil {
		fmt.Println(err)
	}

	resp1 = httptest.NewRecorder()
	router.ServeHTTP(resp1, req)
	//fmt.Printf("%+v\n", resp1.Body)
	assert.Equal(t, 200, resp1.Code, "template success")

	// test load /mui-combined.min.js
	req, err = http.NewRequest("GET", "/mui-combined.min.js", nil)
	if err != nil {
		fmt.Println(err)
	}

	resp1 = httptest.NewRecorder()
	router.ServeHTTP(resp1, req)
	//fmt.Printf("%+v\n", resp1.Body)
	assert.Equal(t, 200, resp1.Code, "load js success")

	/**
	test websocket
	**/

	s := httptest.NewServer(router)
	defer s.Close()

	h := http.Header{"X-Forwarded-User": {"user"}}
	d := websocket.Dialer{}
	c, resp, err := d.Dial("ws://"+s.Listener.Addr().String()+"/ws", h)
	/*if err != nil {
		t.Fatal(err)
	}*/
	assert.Equal(t, http.StatusSwitchingProtocols, 101, "bad handshake : websocket access denied")

	h = http.Header{"X-Forwarded-User": {"admin"}}
	d = websocket.Dialer{}
	c, resp, err = d.Dial("ws://"+s.Listener.Addr().String()+"/ws", h)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode, "ok switching connect")

	o := []byte(`{"CMD":"start","Pass":"qwerty1Q","File":"12-02-2018","Pre":"a"}`)
	//err = c.WriteJSON(o)
	err = c.WriteMessage(websocket.TextMessage, o)
	if err != nil {
		t.Fatal(err)
	}

	// _, respws, er := c.ReadMessage()
	var respws Res
	c.ReadJSON(&respws)
	// fmt.Printf("[test resp] %+v\n", respws)
	assert.Equal(t, "qwerty1Q", respws.Pass, "test return passwd")

	o = []byte(`{"CMD":"stop"}`)
	err = c.WriteMessage(websocket.TextMessage, o)
	if err != nil {
		t.Fatal(err)
	}
	c.ReadJSON(&respws)
	// fmt.Printf("[test resp] %+v\n", respws)
	assert.Equal(t, "stop", respws.Status, "test stop cmd")

	/*	m.HandleMessage(func(s *melody.Session, msg []byte) {
			//fmt.Printf("%+v\n", string(msg))
			_, _, p, _, _ := ParseEntry(msg)
			//fmt.Printf("=== %s %s %s ==\n", e, h, p)
			assert.Equal(t, "qwerty1Q", p, "http success")
			m.Broadcast([]byte(p))
		})
	*/
}
