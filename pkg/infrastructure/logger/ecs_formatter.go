package logger

import (
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

const (
	// ecsVersion tracks the latest ECS release used by this formatter.
	ecsVersion = "9.3.0"

	// ECS examples use millisecond precision with an RFC3339 timezone offset.
	ecsTimestampFormat = "2006-01-02T15:04:05.000Z07:00"
)

var ecsFieldMap = logrus.FieldMap{
	logrus.FieldKeyTime:  "@timestamp",
	logrus.FieldKeyMsg:   "message",
	logrus.FieldKeyLevel: "log.level",
}

// ECSFormatter formats logrus entries as ECS JSON.
type ECSFormatter struct {
	DisableHTMLEscape bool
	DataKey           string
	CallerPrettyfier  func(*runtime.Frame) (function string, file string)
	PrettyPrint       bool
}

type ecsErrorObject struct {
	Message    string `json:"message,omitempty"`
	Type       string `json:"type,omitempty"`
	StackTrace string `json:"stack_trace,omitempty"`
}

// Format converts a logrus entry into ECS-compliant JSON.
func (f *ECSFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	data := f.buildFields(entry)
	data["ecs.version"] = ecsVersion

	ecopy := *entry
	ecopy.Data = data
	ecopy.Caller = nil

	return f.jsonFormatter().Format(&ecopy)
}

func (f *ECSFormatter) buildFields(entry *logrus.Entry) logrus.Fields {
	data := make(logrus.Fields, f.fieldCapacity(entry))
	f.addEntryData(data, entry.Data)
	f.addCallerFields(data, entry)
	return data
}

func (f *ECSFormatter) fieldCapacity(entry *logrus.Entry) int {
	if f.DataKey != "" {
		return 2
	}
	return len(entry.Data)
}

func (f *ECSFormatter) addEntryData(data logrus.Fields, entryData logrus.Fields) {
	if len(entryData) == 0 {
		return
	}

	extraData := data
	if f.DataKey != "" {
		extraData = make(logrus.Fields, len(entryData))
	}

	for key, value := range entryData {
		if f.addErrorField(data, key, value) {
			continue
		}
		extraData[key] = value
	}

	if f.DataKey != "" && len(extraData) > 0 {
		data[f.DataKey] = extraData
	}
}

func (f *ECSFormatter) addErrorField(data logrus.Fields, key string, value any) bool {
	if key != logrus.ErrorKey {
		return false
	}

	err, ok := value.(error)
	if !ok {
		return false
	}

	data["error"] = buildECSErrorObject(err)
	return true
}

func (f *ECSFormatter) addCallerFields(data logrus.Fields, entry *logrus.Entry) {
	if !entry.HasCaller() {
		return
	}

	functionName, fileName, lineNumber := formatCaller(entry.Caller, f.CallerPrettyfier)
	if functionName != "" {
		data["log.origin.function"] = functionName
	}
	if fileName != "" {
		data["log.origin.file.name"] = fileName
	}
	if lineNumber > 0 {
		data["log.origin.file.line"] = lineNumber
	}
}

func (f *ECSFormatter) jsonFormatter() *logrus.JSONFormatter {
	return &logrus.JSONFormatter{
		TimestampFormat:   ecsTimestampFormat,
		DisableHTMLEscape: f.DisableHTMLEscape,
		FieldMap:          ecsFieldMap,
		CallerPrettyfier:  f.CallerPrettyfier,
		PrettyPrint:       f.PrettyPrint,
	}
}

func buildECSErrorObject(err error) ecsErrorObject {
	if err == nil {
		return ecsErrorObject{}
	}

	object := ecsErrorObject{
		Message: err.Error(),
		Type:    reflect.TypeOf(err).String(),
	}

	if stackTrace := extractErrorStackTrace(err); stackTrace != "" {
		object.StackTrace = stackTrace
	}

	return object
}

func extractErrorStackTrace(err error) string {
	stackTrace := fmt.Sprintf("%+v", err)
	if stackTrace == "" || stackTrace == err.Error() {
		return ""
	}

	return stackTrace
}

func formatCaller(frame *runtime.Frame, prettyfier func(*runtime.Frame) (function string, file string)) (string, string, int) {
	if frame == nil {
		return "", "", 0
	}

	var functionName string
	var fileName string
	var lineNumber int

	if prettyfier != nil {
		functionName, fileName = prettyfier(frame)
		if separator := strings.LastIndex(fileName, ":"); separator != -1 {
			if parsedLine, err := strconv.Atoi(fileName[separator+1:]); err == nil {
				fileName = fileName[:separator]
				lineNumber = parsedLine
			}
		}
	} else {
		functionName = frame.Function
		fileName = frame.File
		lineNumber = frame.Line
	}

	if fileName != "" {
		fileName = filepath.Base(fileName)
	}

	return functionName, fileName, lineNumber
}
