/*
Copyright Neo4j.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package logging configures operator logr/zap sinks (ADR-014 + optional file tee).
package logging

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Options configures stdout essentials vs optional verbose file sink.
type Options struct {
	// Zap holds controller-runtime zap flags (--zap-devel, --zap-log-level, …).
	Zap crzap.Options

	// FilePath, when non-empty, tees logs to that file (created/appended).
	FilePath string
	// FileLevel is the minimum level written to FilePath (default debug).
	FileLevel string
}

// BindFlags registers zap flags plus --log-file / --log-file-level.
func (o *Options) BindFlags(fs *flag.FlagSet) {
	o.Zap.BindFlags(fs)
	fs.StringVar(&o.FilePath, "log-file", o.FilePath,
		"Optional path to tee operator logs (essentials stay on stderr; file may be more verbose via --log-file-level)")
	fs.StringVar(&o.FileLevel, "log-file-level", o.FileLevel,
		"Minimum level for --log-file (debug|info|error). Default: debug when --log-file is set")
}

// New builds a logr.Logger. The returned closer flushes/closes the optional file sink.
func New(o Options) (logr.Logger, func(), error) {
	if o.FilePath == "" {
		return crzap.New(crzap.UseFlagOptions(&o.Zap)), func() {}, nil
	}

	fileLevel, err := parseLevel(o.FileLevel, zapcore.DebugLevel)
	if err != nil {
		return logr.Logger{}, nil, fmt.Errorf("log-file-level: %w", err)
	}

	f, err := os.OpenFile(o.FilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return logr.Logger{}, nil, fmt.Errorf("open log-file %q: %w", o.FilePath, err)
	}

	stdoutLevel := stdoutLevelFrom(&o.Zap)
	enc := newEncoder(o.Zap.Development)

	tee := zapcore.NewTee(
		zapcore.NewCore(enc, zapcore.Lock(os.Stderr), stdoutLevel),
		zapcore.NewCore(enc.Clone(), zapcore.AddSync(f), fileLevel),
	)
	zl := zap.New(tee, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	closer := func() {
		_ = zl.Sync()
		_ = f.Close()
	}
	return zapr.NewLogger(zl), closer, nil
}

func stdoutLevelFrom(z *crzap.Options) zapcore.LevelEnabler {
	if z.Level != nil {
		return z.Level
	}
	if z.Development {
		return zapcore.DebugLevel
	}
	return zapcore.InfoLevel
}

func newEncoder(devel bool) zapcore.Encoder {
	if devel {
		cfg := zap.NewDevelopmentEncoderConfig()
		cfg.EncodeTime = zapcore.ISO8601TimeEncoder
		return zapcore.NewConsoleEncoder(cfg)
	}
	cfg := zap.NewProductionEncoderConfig()
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.MessageKey = "msg"
	cfg.LevelKey = "level"
	return zapcore.NewJSONEncoder(cfg)
}

func parseLevel(s string, fallback zapcore.Level) (zapcore.Level, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return fallback, nil
	}
	var lvl zapcore.Level
	if err := lvl.UnmarshalText([]byte(s)); err != nil {
		return fallback, fmt.Errorf("invalid level %q (want debug|info|error)", s)
	}
	return lvl, nil
}
