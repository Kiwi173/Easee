package util

import (
	"bytes"
	"io"
	"log"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	jww "github.com/spf13/jwalterweatherman"
)

// Logger wraps a jww notepad to avoid leaking implementation detail
type Logger interface {
	ErrorLogger() *log.Logger
	TraceLogger() *log.Logger

	Name() string
	Redact(...string)

	Traceln(v ...interface{})
	Tracef(format string, v ...interface{})
	Debugln(v ...interface{})
	Debugf(format string, v ...interface{})
	Infoln(v ...interface{})
	Infof(format string, v ...interface{})
	Warnln(v ...interface{})
	Warnf(format string, v ...interface{})
	Errorln(v ...interface{})
	Errorf(format string, v ...interface{})
	Fatalln(v ...interface{})
	Fatalf(format string, v ...interface{})
}

var (
	loggers = map[string]*logger{}
	levels  = map[string]jww.Threshold{}

	loggersMux sync.Mutex

	// OutThreshold is the default console log level
	OutThreshold = jww.LevelError

	// LogThreshold is the default log file level
	LogThreshold = jww.LevelWarn
)

// LogAreaPadding of log areas
var LogAreaPadding = 6

var _ Logger = (*logger)(nil)

// Logger wraps a jww notepad to avoid leaking implementation detail
type logger struct {
	*jww.Notepad
	name string
}

type redactor struct {
	r [][]byte
	w io.Writer
}

var _ io.Writer = (*redactor)(nil)

func (r *redactor) Write(p []byte) (int, error) {
	b := p
	for _, r := range r.r {
		b = bytes.ReplaceAll(b, r, []byte("***"))
	}
	return r.w.Write(b)
}

// NewLogger creates a logger with the given log area and adds it to the registry
func NewLogger(area string) Logger {
	padded := area
	for len(padded) < LogAreaPadding {
		padded = padded + " "
	}

	level := LogLevelForArea(area)
	notepad := jww.NewNotepad(level, level, os.Stdout, io.Discard, padded, log.Ldate|log.Ltime)

	loggersMux.Lock()
	defer loggersMux.Unlock()

	logger := &logger{
		Notepad: notepad,
		name:    area,
	}
	loggers[area] = logger
	return logger
}

func (l *logger) ErrorLogger() *log.Logger {
	return l.ERROR
}
func (l *logger) TraceLogger() *log.Logger {
	return l.TRACE
}

func (l *logger) Traceln(v ...interface{}) {
	l.TRACE.Println(v...)
}
func (l *logger) Tracef(format string, v ...interface{}) {
	l.TRACE.Printf(format, v...)
}
func (l *logger) Debugln(v ...interface{}) {
	l.DEBUG.Println(v...)
}
func (l *logger) Debugf(format string, v ...interface{}) {
	l.DEBUG.Printf(format, v...)
}
func (l *logger) Infoln(v ...interface{}) {
	l.INFO.Println(v...)
}
func (l *logger) Infof(format string, v ...interface{}) {
	l.INFO.Printf(format, v...)
}
func (l *logger) Warnln(v ...interface{}) {
	l.WARN.Println(v...)
}
func (l *logger) Warnf(format string, v ...interface{}) {
	l.WARN.Printf(format, v...)
}
func (l *logger) Errorln(v ...interface{}) {
	l.ERROR.Println(v...)
}
func (l *logger) Errorf(format string, v ...interface{}) {
	l.ERROR.Printf(format, v...)
}
func (l *logger) Fatalln(v ...interface{}) {
	l.FATAL.Println(v...)
}
func (l *logger) Fatalf(format string, v ...interface{}) {
	l.FATAL.Printf(format, v...)
}

// Redact returns the loggers name
func (l *logger) Redact(r ...string) {
	red := &redactor{w: l.TRACE.Writer()}
	for _, r := range r {
		red.r = append(red.r, []byte(r), []byte(url.QueryEscape(r)))
	}
	l.TRACE.SetOutput(red)
}

// Name returns the loggers name
func (l *logger) Name() string {
	return l.name
}

// Loggers invokes callback for each configured logger
func Loggers(cb func(string, *logger)) {
	for name, logger := range loggers {
		cb(name, logger)
	}
}

// LogLevelForArea gets the log level for given log area
func LogLevelForArea(area string) jww.Threshold {
	level, ok := levels[strings.ToLower(area)]
	if !ok {
		level = OutThreshold
	}
	return level
}

// LogLevel sets log level for all loggers
func LogLevel(defaultLevel string, areaLevels map[string]string) {
	// default level
	OutThreshold = LogLevelToThreshold(defaultLevel)
	LogThreshold = OutThreshold

	// area levels
	for area, level := range areaLevels {
		area = strings.ToLower(area)
		levels[area] = LogLevelToThreshold(level)
	}

	Loggers(func(name string, logger *logger) {
		logger.SetStdoutThreshold(LogLevelForArea(name))
	})
}

// LogLevelToThreshold converts log level string to a jww Threshold
func LogLevelToThreshold(level string) jww.Threshold {
	switch strings.ToUpper(level) {
	case "FATAL":
		return jww.LevelFatal
	case "ERROR":
		return jww.LevelError
	case "WARN":
		return jww.LevelWarn
	case "INFO":
		return jww.LevelInfo
	case "DEBUG":
		return jww.LevelDebug
	case "TRACE":
		return jww.LevelTrace
	default:
		panic("invalid log level " + level)
	}
}

var uiChan chan<- Param

type uiWriter struct {
	re    *regexp.Regexp
	level string
}

func (w *uiWriter) Write(p []byte) (n int, err error) {
	// trim level and timestamp
	s := string(w.re.ReplaceAll(p, []byte{}))

	uiChan <- Param{
		Key: w.level,
		Val: strings.Trim(strconv.Quote(strings.TrimSpace(s)), "\""),
	}

	return 0, nil
}

// CaptureLogs appends uiWriter to relevant log levels
func CaptureLogs(c chan<- Param) {
	uiChan = c

	for _, l := range loggers {
		captureLogger("warn", l.Notepad.WARN)
		captureLogger("error", l.Notepad.ERROR)
		captureLogger("error", l.Notepad.FATAL)
	}
}

func captureLogger(level string, l *log.Logger) {
	re, err := regexp.Compile(`^\[[a-zA-Z0-9-]+\s*\] \w+ .{19} `)
	if err != nil {
		panic(err)
	}

	ui := uiWriter{
		re:    re,
		level: level,
	}

	mw := io.MultiWriter(l.Writer(), &ui)
	l.SetOutput(mw)
}
