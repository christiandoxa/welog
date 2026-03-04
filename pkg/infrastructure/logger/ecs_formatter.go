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
	datahint := len(entry.Data)
	if f.DataKey != "" {
		datahint = 2
	}

	data := make(logrus.Fields, datahint)
	if len(entry.Data) > 0 {
		extraData := data
		if f.DataKey != "" {
			extraData = make(logrus.Fields, len(entry.Data))
		}

		for k, v := range entry.Data {
			switch k {
			case logrus.ErrorKey:
				if err, ok := v.(error); ok {
					data["error"] = buildECSErrorObject(err)
					continue
				}
			}

			extraData[k] = v
		}

		if f.DataKey != "" && len(extraData) > 0 {
			data[f.DataKey] = extraData
		}
	}

	if entry.HasCaller() {
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

	data["ecs.version"] = ecsVersion

	ecopy := *entry
	ecopy.Data = data
	ecopy.Caller = nil

	formatter := logrus.JSONFormatter{
		TimestampFormat:   ecsTimestampFormat,
		DisableHTMLEscape: f.DisableHTMLEscape,
		FieldMap:          ecsFieldMap,
		CallerPrettyfier:  f.CallerPrettyfier,
		PrettyPrint:       f.PrettyPrint,
	}

	return formatter.Format(&ecopy)
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
