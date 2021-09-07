package util

import (
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/evcc-io/evcc/api"
	jww "github.com/spf13/jwalterweatherman"
)

var (
	loggers = map[string]*Logger{}
	levels  = map[string]jww.Threshold{}

	loggersMux sync.Mutex

	// OutThreshold is the default console log level
	OutThreshold = jww.LevelError

	// LogThreshold is the default log file level
	LogThreshold = jww.LevelWarn
)

// LogAreaPadding of log areas
var LogAreaPadding = 6

// Logger wraps a jww notepad to avoid leaking implementation detail
type Logger struct {
	*jww.Notepad
	name string
}

// NewLogger creates a logger with the given log area and adds it to the registry
func NewLogger(area string) *Logger {
	loggersMux.Lock()
	defer loggersMux.Unlock()

	if logger, ok := loggers[area]; ok {
		return logger
	}

	padded := area
	for len(padded) < LogAreaPadding {
		padded = padded + " "
	}

	level := LogLevelForArea(area)
	notepad := jww.NewNotepad(level, level, os.Stdout, io.Discard, padded, log.Ldate|log.Ltime)

	logger := &Logger{
		Notepad: notepad,
		name:    area,
	}
	loggers[area] = logger
	return logger
}

// implement api.Logger
var _ api.Logger = (*Logger)(nil)

func (l *Logger) Tracef(format string, v ...interface{}) {
	l.TRACE.Printf(format, v...)
}

func (l *Logger) Traceln(v ...interface{}) {
	l.TRACE.Println(v...)
}

func (l *Logger) Debugf(format string, v ...interface{}) {
	l.DEBUG.Printf(format, v...)
}

func (l *Logger) Debugln(v ...interface{}) {
	l.DEBUG.Println(v...)
}

func (l *Logger) Infof(format string, v ...interface{}) {
	l.INFO.Printf(format, v...)
}

func (l *Logger) Infoln(v ...interface{}) {
	l.INFO.Println(v...)
}

func (l *Logger) Warnf(format string, v ...interface{}) {
	l.WARN.Printf(format, v...)
}

func (l *Logger) Warnln(v ...interface{}) {
	l.WARN.Println(v...)
}

func (l *Logger) Errorf(format string, v ...interface{}) {
	l.ERROR.Printf(format, v...)
}

func (l *Logger) Errorln(v ...interface{}) {
	l.ERROR.Println(v...)
}

func (l *Logger) Fatalf(format string, v ...interface{}) {
	l.FATAL.Fatalf(format, v...)
}

func (l *Logger) Fatalln(v ...interface{}) {
	l.FATAL.Fatalln(v...)
}

// Name returns the loggers name
func (l *Logger) Name() string {
	return l.name
}

// Loggers invokes callback for each configured logger
func Loggers(cb func(string, *Logger)) {
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

	Loggers(func(name string, logger *Logger) {
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
