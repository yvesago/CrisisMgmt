package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/foolin/gin-template"
	"github.com/gin-gonic/gin"
	"github.com/itsjamie/gin-cors"
	"gopkg.in/olahol/melody.v1"
	//"github.com/olahol/melody"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"
	"unicode"
)

// Config : struct to read json config file
type Config struct {
	Port       string
	Server     string // server url for html template
	User       string // user id for processes auth
	CorsOrigin string
	PublicDir  string // [files/] for URL
	PublicPath string // [/home/files/] real path to write links
	MngmtDir   string // [mngmt/] protected websocket dir
	AuthAdmins []string
	LogFile    string // CrisisMgmt logs, not used with debug
}

// Res : status of current process
type Res struct {
	Status     string   `json:"cmdstatus"`
	User       string   `json:"user"`       // user id for processes auth
	Pass       string   `json:"pass"`       // pass for processes auth
	Admin      string   `json:"admin"`      // admin id who launch processes
	Time       string   `json:"time"`       // start time
	File       string   `json:"file"`       // current working files
	PublicFile string   `json:"publicfile"` // for URL
	Error      string   `json:"error"`
	PublicPath string   // full path of processes files
	Files      []string `json:"files"` // list of usable files
	ProcLog    *exec.Cmd
	ProcBoard  *exec.Cmd
}

func randStringBytes(n int) string {
	const letterBytes = "abcdefghijkmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	//const letterBytes = "abcdefghijkmnopqrstuvwxyz23456789" // simpliest password
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

// IsLetter : test single letter
var IsLetter = regexp.MustCompile(`^[a-zA-Z]$`).MatchString

// IsSafeChar : test simple chars
var IsSafeChar = regexp.MustCompile(`^[a-zA-Z0-9-]+$`).MatchString

//var IsDate = regexp.MustCompile(`^\d{2}-\d{2}-\d{4}$`).MatchString

func verifyPassword(s string) (sevenOrMore, number, upper, special bool) {
	letters := 0
	for _, s := range s {
		switch {
		case unicode.IsNumber(s):
			number = true
		case unicode.IsUpper(s):
			upper = true
			letters++
		case unicode.IsPunct(s) || unicode.IsSymbol(s):
			special = true
		case unicode.IsLetter(s) || s == ' ':
			letters++
		default:
			//return false, false, false, false
		}
	}
	sevenOrMore = letters >= 7
	return
}

func contains(s []string, t string) bool {
	for _, v := range s {
		if v == t {
			return true
		}
	}
	return false
}

// ParseEntry : parse and control entries
func ParseEntry(msg []byte) (string, string, string, string, string) {

	type Info struct {
		CMD  string `json:"CMD"`
		Pass string `json:"Pass"`
		File string `json:"File"`
		Pre  string `json:"Pre"`
	}

	var objmap Info

	err := ""
	e := json.Unmarshal(msg, &objmap)
	if e != nil {
		err = fmt.Sprintf("%s", e)
		objmap.CMD = "error"
	}

	validCmd := []string{"init", "start", "stop", "error"}
	if contains(validCmd, objmap.CMD) == false {
		err = "Bad cmd"
		objmap.CMD = "error"
	}

	if objmap.File != "" && objmap.Pre != "" {
		objmap.Pre = "" // pre is only usable for new files
	}

	if (objmap.Pre != "") && (IsLetter(objmap.Pre) == false) {
		err = "Bad prefix"
		objmap.CMD = "error"
		objmap.Pre = ""
	}

	sevenOrMore, number, upper, _ := verifyPassword(objmap.Pass)
	validPass := sevenOrMore && number && upper
	if objmap.CMD == "start" && validPass == false {
		err = "Bad password"
		objmap.CMD = "error"
		objmap.Pass = ""
	}

	if objmap.File != "" && IsSafeChar(objmap.File) == false {
		err = "Bad file name"
		objmap.CMD = "error"
		objmap.File = ""
	}

	// fmt.Printf("== %s ==\n", err)

	return err, objmap.CMD, objmap.Pass, objmap.File, objmap.Pre
}

func readFiles() []string {
	r, _ := regexp.Compile("files/(.*?).sql")
	files, ef := filepath.Glob("files/*.sql")
	if ef != nil {
		log.Fatal(ef)
	}

	var listFiles []string
	for _, file := range files {
		match := r.FindStringSubmatch(file)
		fmt.Println(match[1])
		listFiles = append(listFiles, match[1])
	}
	return listFiles
}

// StopProc : remove links and stop processes
func StopProc(res Res) Res {
	cl := res.ProcLog
	cb := res.ProcBoard

	res.Status = "stop"
	res.Pass = ""
	res.User = ""
	res.PublicFile = ""
	t := time.Now()
	res.Time = fmt.Sprintf("%s", t.Format("02-01-2006 15:04"))

	os.Remove(res.PublicPath + ".log")
	os.Remove(res.PublicPath + ".sql")

	if err := cl.Process.Kill(); err != nil {
		log.Println(err)
		res.Error = err.Error()
		return res
	}
	cl.Wait()
	if err := cb.Process.Kill(); err != nil {
		log.Println(err)
		res.Error = err.Error()
		return res
	}
	cb.Wait()

	return res
}

// StartProc : start processes and create links for public download
func StartProc(res Res, file string, pre string, pass string, debug bool, config Config) Res {
	logpath := "./CrisisLog"
	boardpath := "./CrisisBoard"
	serv := config.Server
	u := config.User
	dflag := ""
	if debug == true {
		dflag = "-d"
	}

	// create or select file
	filename := ""
	if file == "" {
		t := time.Now()
		day := t.Format("02-01-2006")
		filename = "files/" + pre + day

		res.Files = append(res.Files, pre+day)
	} else {
		filename = "files/" + file
	}

	// start proc
	t := time.Now()
	res.Time = fmt.Sprintf("%s", t.Format("02-01-2006 15:04"))

	var cl *exec.Cmd
	var cb *exec.Cmd

	cl = exec.Command(logpath, "-s", serv+"/log/",
		"-u", u, "-p", "5001", "-f", filename+".log", dflag)
	cl.Env = os.Environ()
	cl.Env = append(cl.Env, "CRISIS_KEY="+pass)
	if err := cl.Start(); err != nil {
		log.Println(err)
		res.Status = "stop"
		res.Error = err.Error()
		return res
	}
	cb = exec.Command(boardpath, "-s", serv+"/board/",
		"-u", u, "-p", "5000", "-f", filename+".sql", dflag)
	cb.Env = os.Environ()
	cb.Env = append(cb.Env, "CRISIS_KEY="+pass)
	if err := cb.Start(); err != nil {
		log.Println(err)
		res.Status = "stop"
		res.Error = err.Error()
		return res
	}
	res.ProcLog = cl
	res.ProcBoard = cb

	// set links
	randfile := randStringBytes(12)
	res.PublicFile = config.PublicDir + "/" + randfile // for URL
	pwd, _ := os.Getwd()
	path := config.PublicPath + "/" + randfile
	res.PublicPath = path // for stop processes
	os.Symlink(pwd+"/"+filename+".sql", path+".sql")
	os.Symlink(pwd+"/"+filename+".log", path+".log")

	// return
	res.Pass = pass
	res.User = u
	res.File = filename
	res.Status = "start"
	return res
}

func server(r *gin.Engine, debug bool, config Config) {

	var res Res
	res.Files = readFiles()

	m := melody.New()

	r.Use(cors.Middleware(cors.Config{
		Origins:         config.CorsOrigin,
		Methods:         "GET, PUT",
		RequestHeaders:  "Origin, Content-Type",
		MaxAge:          50 * time.Second,
		Credentials:     true,
		ValidateHeaders: false,
	}))

	r.GET("/mui-combined.min.js", func(c *gin.Context) {
		http.ServeFile(c.Writer, c.Request, "mui-combined.min.js")
	})

	r.HTMLRender = gintemplate.Default()

	r.GET("/", func(c *gin.Context) {
		// http.ServeFile(c.Writer, c.Request, "index.html")
		access := false
		for _, u := range c.Request.Header["X-Forwarded-User"] {
			if contains(config.AuthAdmins, u) == true {
				access = true
			}
		}

		// allow access without auth if AuthAdmins contains ""
		if len(c.Request.Header["X-Forwarded-User"]) == 0 && contains(config.AuthAdmins, "") {
			access = true
		}

		if access == false {
			c.JSON(401, "Access denied")
			c.Abort()
		} else {
			c.HTML(200, "index.html", gin.H{
				"IP":     c.ClientIP(),
				"wspath": config.MngmtDir,
				"serv":   config.Server,
				"user":   c.Request.Header["X-Forwarded-User"],
			})
		}
	})

	r.GET("/ws", func(c *gin.Context) {
		ml := make(map[string]interface{})
		ml["cip"] = c.ClientIP()
		ml["user"] = c.Request.Header["X-Forwarded-User"]
		m.HandleRequestWithKeys(c.Writer, c.Request, ml)
	})

	m.HandleMessage(func(s *melody.Session, msg []byte) {
		t := time.Now()
		ip, _ := s.Get("cip")
		ru, _ := s.Get("user")

		if debug == true {
			fmt.Printf("%s: %s - %+v\n %+v\n", t.Format("02-01-2006 15:04"), ip, ru, string(msg))
		}

		emsg, hcmd, pass, file, pre := ParseEntry(msg)

		res.Error = emsg
		res.Admin = fmt.Sprintf("%s", ru)

		// log
		log.Printf("%s: %s - %s, cmd: %s, file: %s%s, err: %s\n",
			t.Format("02-01-2006 15:04:05"),
			ip, ru,
			hcmd, pre, file, emsg)

		switch hcmd {
		case "error":
			res.Status = "stop"
			break
		case "stop":
			if res.Status == "stop" {
				break
			}
			res = StopProc(res)
		case "start":
			if res.Status == "start" {
				break
			}
			res = StartProc(res, file, pre, pass, debug, config)
		default:
		}
		br, _ := json.Marshal(res)
		//fmt.Printf("resp %+v\n", res)
		m.Broadcast([]byte(br))
		res.Error = ""
	})

}

func init() {
	rand.Seed(time.Now().UnixNano())
}

func main() {

	var Usage = func() {
		fmt.Fprintf(os.Stderr, "\nUsage of %s\n\n  Default behaviour: start server\n\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Println()
		os.Exit(0)
	}

	confPtr := flag.String("conf", "", "*Mandatory* Json config file")
	debugPtr := flag.Bool("d", false, "Debug mode")
	flag.Parse()

	debug := *debugPtr
	conf := *confPtr

	if conf == "" {
		fmt.Fprintf(os.Stderr, "=========\nMissing mandatory config file\n=========\n")
		Usage()
	}

	// Load config from file
	file, err := os.Open(conf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "=========\nError: %s\n=========\n", err)
		Usage()
	}

	decoder := json.NewDecoder(file)
	config := Config{}
	err = decoder.Decode(&config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "=========\nError: %s\n=========\n", err)
		Usage()
	}

	logFile, erl := os.OpenFile(config.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0664)
	if erl != nil {
		fmt.Fprintf(os.Stderr, "=========\nError log file: %s\n=========\n", erl)
		Usage()
	}

	if debug == false {
		gin.SetMode(gin.ReleaseMode)
		gin.DefaultWriter = io.MultiWriter(logFile)
		//gin.DefaultWriter = io.MultiWriter(logFile, os.Stdout)
	}

	log.SetOutput(gin.DefaultWriter)

	r := gin.New()
	r.Use(gin.Recovery())

	if debug == true {
		r.Use(gin.Logger())
	}

	server(r, debug, config)

	r.Run(":" + config.Port)
}
