package chug

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"code.cloudfoundry.org/lager"
)

type Entry struct {
	IsLager bool
	Raw     []byte
	Log     LogEntry
}

type LogEntry struct {
	Timestamp time.Time
	LogLevel  lager.LogLevel

	Source  string
	Message string
	Session string

	Error error
	Trace string

	Data lager.Data
}

func Chug(reader io.Reader, out chan<- Entry) {
	scanner := bufio.NewReader(reader)
	for {
		line, err := scanner.ReadBytes('\n')
		if line != nil {
			out <- entry(bytes.TrimSuffix(line, []byte{'\n'}))
		}
		if err != nil {
			break
		}
	}
	close(out)
}

func entry(raw []byte) (entry Entry) {
	copiedBytes := make([]byte, len(raw))
	copy(copiedBytes, raw)
	entry = Entry{
		IsLager: false,
		Raw:     copiedBytes,
	}

	rawString := string(raw)
	idx := strings.Index(rawString, "{")
	if idx == -1 {
		return
	}

	var lagerLog lager.LogFormat

	decoder := json.NewDecoder(strings.NewReader(rawString[idx:]))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&lagerLog)
	if err != nil {
		var prettyLog lager.PrettyFormat
		decoder = json.NewDecoder(strings.NewReader(rawString[idx:]))
		err = decoder.Decode(&prettyLog)
		if err != nil {
			return
		}
		entry.Log, entry.IsLager = convertPrettyLog(prettyLog)
	} else {
		entry.Log, entry.IsLager = convertLagerLog(lagerLog)
	}

	return
}

func convertLagerLog(lagerLog lager.LogFormat) (LogEntry, bool) {
	trace, err := traceFromData(lagerLog.Data)
	if err != nil {
		return LogEntry{}, false
	}

	session, err := sessionFromData(lagerLog.Data)
	if err != nil {
		return LogEntry{}, false
	}

	var logErr error
	if lagerLog.LogLevel == lager.ERROR || lagerLog.LogLevel == lager.FATAL {
		logErr, err = errorFromData(lagerLog.Data)
		if err != nil {
			return LogEntry{}, false
		}
	}

	timestamp, err := strconv.ParseFloat(lagerLog.Timestamp, 64)
	if err != nil {
		return LogEntry{}, false
	}

	return LogEntry{
		Timestamp: time.Unix(0, int64(timestamp*1e9)),
		LogLevel:  lagerLog.LogLevel,
		Source:    lagerLog.Source,
		Message:   lagerLog.Message,
		Session:   session,

		Error: logErr,
		Trace: trace,

		Data: lagerLog.Data,
	}, true
}

func convertPrettyLog(lagerLog lager.PrettyFormat) (LogEntry, bool) {
	trace, err := traceFromData(lagerLog.Data)
	if err != nil {
		return LogEntry{}, false
	}

	session, err := sessionFromData(lagerLog.Data)
	if err != nil {
		return LogEntry{}, false
	}

	logLevel, err := lager.LogLevelFromString(lagerLog.Level)
	if err != nil {
		return LogEntry{}, false
	}

	var logErr error
	if logLevel == lager.ERROR || logLevel == lager.FATAL {
		logErr, err = errorFromData(lagerLog.Data)
		if err != nil {
			return LogEntry{}, false
		}
	}

	return LogEntry{
		Timestamp: time.Time(lagerLog.Timestamp),
		LogLevel:  logLevel,
		Source:    lagerLog.Source,
		Message:   lagerLog.Message,
		Session:   session,

		Error: logErr,
		Trace: trace,

		Data: lagerLog.Data,
	}, true
}

func traceFromData(data lager.Data) (string, error) {
	trace, ok := data["trace"]
	if ok {
		traceString, ok := trace.(string)
		if !ok {
			return "", fmt.Errorf("unable to convert trace: %v", trace)
		}
		delete(data, "trace")
		return traceString, nil
	}
	return "", nil
}

func sessionFromData(data lager.Data) (string, error) {
	session, ok := data["session"]
	if ok {
		sessionString, ok := session.(string)
		if !ok {
			return "", fmt.Errorf("unable to convert session: %v", session)
		}
		delete(data, "session")
		return sessionString, nil
	}
	return "", nil
}

func errorFromData(data lager.Data) (error, error) {
	err, ok := data["error"]
	if ok {
		errorString, ok := err.(string)
		if !ok {
			return nil, fmt.Errorf("unable to convert error: %v", err)
		}
		delete(data, "error")
		return errors.New(errorString), nil
	}
	return nil, nil
}
