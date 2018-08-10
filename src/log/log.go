// Copyright © 2018 VMware, Inc. All Rights Reserved.
// SPDX-License-Identifier: MIT

package log

import (
	"fmt"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"time"
)

var core zapcore.Core

func Initialize() error {

	logger, _ := zap.NewProduction()

	cfg := zap.NewProductionConfig()
	cfg.OutputPaths = []string{
		"/var/log/syndicate.log",
		"stderr",
	}
	cfg.ErrorOutputPaths = []string{
		"/var/log/syndicate.log",
		"stderr",
	}
	logger, _ = cfg.Build()
	_ = zap.ReplaceGlobals(logger)
	_ = zap.RedirectStdLog(logger)
	core = logger.Core()
	return nil
}

func Infof(msg string, args ...interface{}) {
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}
	e := zapcore.Entry{
		Message:    msg,
		Level:      zapcore.InfoLevel,
		Time:       time.Now().UTC(),
		LoggerName: "syndicate",
	}
	core.Write(e, nil)
}

func Debugf(msg string, args ...interface{}) {
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}
	e := zapcore.Entry{
		Message:    msg,
		Level:      zapcore.DebugLevel,
		Time:       time.Now().UTC(),
		LoggerName: "syndicate",
	}
	core.Write(e, nil)
}

func Errorf(msg string, args ...interface{}) {
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}
	e := zapcore.Entry{
		Message:    msg,
		Level:      zapcore.ErrorLevel,
		Time:       time.Now().UTC(),
		LoggerName: "syndicate",
	}
	core.Write(e, nil)
}
