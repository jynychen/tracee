package derive

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aquasecurity/tracee/pkg/events"
	"github.com/aquasecurity/tracee/pkg/logger"
	"github.com/aquasecurity/tracee/pkg/utils/sharedobjs"
	"github.com/aquasecurity/tracee/types/trace"
)

type testSOInstance struct {
	info sharedobjs.ObjInfo
	syms []string
}

type symbolsLoaderMock struct {
	cache         map[sharedobjs.ObjInfo]map[string]bool
	returnedError error
}

func initLoaderMock(returnedError error) symbolsLoaderMock {
	return symbolsLoaderMock{cache: make(map[sharedobjs.ObjInfo]map[string]bool), returnedError: returnedError}
}

func (loader symbolsLoaderMock) GetDynamicSymbols(info sharedobjs.ObjInfo) (map[string]bool, error) {
	if loader.returnedError != nil {
		return nil, loader.returnedError
	}
	return loader.cache[info], nil
}

func (loader symbolsLoaderMock) GetExportedSymbols(info sharedobjs.ObjInfo) (map[string]bool, error) {
	if loader.returnedError != nil {
		return nil, loader.returnedError
	}
	return loader.cache[info], nil
}

func (loader symbolsLoaderMock) GetImportedSymbols(info sharedobjs.ObjInfo) (map[string]bool, error) {
	return nil, nil
}

func (loader symbolsLoaderMock) GetLocalSymbols(info sharedobjs.ObjInfo) (map[string]bool, error) {
	return nil, nil
}

func (loader symbolsLoaderMock) addSOSymbols(info testSOInstance) {
	symsMap := make(map[string]bool)
	for _, s := range info.syms {
		symsMap[s] = true
	}
	loader.cache[info.info] = symsMap
}

func generateSOLoadedEvent(pid int, so sharedobjs.ObjInfo) trace.Event {
	return trace.Event{
		EventName:     "shared_object_loaded",
		EventID:       int(events.SharedObjectLoaded),
		HostProcessID: pid,
		ProcessID:     pid,
		Args: []trace.Argument{
			{ArgMeta: trace.ArgMeta{Type: "string", Name: "pathname"}, Value: so.Path},
			{ArgMeta: trace.ArgMeta{Type: "int32", Name: "flags"}, Value: 0},
			{ArgMeta: trace.ArgMeta{Type: "uint32", Name: "dev"}, Value: so.Id.Device},
			{ArgMeta: trace.ArgMeta{Type: "uint64", Name: "inode"}, Value: so.Id.Inode},
			{ArgMeta: trace.ArgMeta{Type: "uint64", Name: "ctime"}, Value: so.Id.Ctime},
		},
	}
}

func TestDeriveSharedObjectExportWatchedSymbols(t *testing.T) {
	happyFlowTestCases := []struct {
		name            string
		watchedSymbols  []string
		whitelistedLibs []string
		loadingSO       testSOInstance
		expectedSymbols []string
	}{
		{
			name:            "SO with no export symbols",
			watchedSymbols:  []string{"open", "close", "write"},
			whitelistedLibs: []string{},
			loadingSO: testSOInstance{
				info: sharedobjs.ObjInfo{Id: sharedobjs.ObjID{Inode: 1}, Path: "1.so"},
				syms: []string{},
			},
			expectedSymbols: []string{},
		},
		{
			name:            "SO with 1 watched export symbols",
			watchedSymbols:  []string{"open", "close", "write"},
			whitelistedLibs: []string{},
			loadingSO: testSOInstance{
				info: sharedobjs.ObjInfo{Id: sharedobjs.ObjID{Inode: 1}, Path: "1.so"},
				syms: []string{"open"},
			},
			expectedSymbols: []string{"open"},
		},
		{
			name:            "SO with multiple watched export symbols",
			watchedSymbols:  []string{"open", "close", "write"},
			whitelistedLibs: []string{},
			loadingSO: testSOInstance{
				info: sharedobjs.ObjInfo{Id: sharedobjs.ObjID{Inode: 1}, Path: "1.so"},
				syms: []string{
					"open",
					"close",
					"write",
				},
			},
			expectedSymbols: []string{"open", "close", "write"},
		},
		{
			name:            "SO with partly watched export symbols",
			watchedSymbols:  []string{"open", "close", "write"},
			whitelistedLibs: []string{},
			loadingSO: testSOInstance{
				info: sharedobjs.ObjInfo{Id: sharedobjs.ObjID{Inode: 1}, Path: "1.so"},
				syms: []string{
					"open",
					"close",
					"sync",
				},
			},
			expectedSymbols: []string{"open", "close"},
		},
		{
			name:            "SO with no watched export symbols",
			watchedSymbols:  []string{"open", "close", "write"},
			whitelistedLibs: []string{},
			loadingSO: testSOInstance{
				info: sharedobjs.ObjInfo{Id: sharedobjs.ObjID{Inode: 1}, Path: "1.so"},
				syms: []string{
					"createdir",
					"rmdir",
					"walk",
				},
			},
			expectedSymbols: []string{},
		},
		{
			name:            "whitelisted full path SO",
			watchedSymbols:  []string{"open", "close", "write"},
			whitelistedLibs: []string{"/tmp/test"},
			loadingSO: testSOInstance{
				info: sharedobjs.ObjInfo{Id: sharedobjs.ObjID{Inode: 1}, Path: "/tmp/test.so"},
				syms: []string{"open"},
			},
			expectedSymbols: []string{},
		},
		{
			name:            "whitelisted SO name",
			watchedSymbols:  []string{"open", "close", "write"},
			whitelistedLibs: []string{"test"},
			loadingSO: testSOInstance{
				info: sharedobjs.ObjInfo{Id: sharedobjs.ObjID{Inode: 1}, Path: "/lib/test.so"},
				syms: []string{"open"},
			},
			expectedSymbols: []string{},
		},
	}
	pid := 1
	baseLogger := logger.Current()

	t.Run("Happy flow", func(t *testing.T) {
		for _, testCase := range happyFlowTestCases {
			testCase := testCase

			errChan := setMockLogger(logger.DebugLevel)
			mockLoader := initLoaderMock(nil)
			mockLoader.addSOSymbols(testCase.loadingSO)

			t.Run(testCase.name, func(t *testing.T) {
				t.Parallel()

				gen := initSymbolsLoadedEventGenerator(mockLoader, testCase.watchedSymbols, testCase.whitelistedLibs)
				event := generateSOLoadedEvent(pid, testCase.loadingSO.info)
				eventArgs, err := gen.deriveArgs(&event)
				assert.Empty(t, errChan)
				require.NoError(t, err)
				if len(testCase.expectedSymbols) > 0 {
					require.Len(t, eventArgs, 3)
					path := eventArgs[0]
					syms := eventArgs[1]
					require.IsType(t, "", path)
					require.IsType(t, []string{}, syms)
					assert.ElementsMatch(t, testCase.expectedSymbols, syms.([]string))
					assert.Equal(t, testCase.loadingSO.info.Path, path.(string))
				} else {
					assert.Len(t, eventArgs, 0)
				}
			})

			logger.SetLogger(baseLogger)
		}
	})

	t.Run("Errors flow", func(t *testing.T) {
		t.Run("Debug", func(t *testing.T) {
			errChan := setMockLogger(logger.DebugLevel)
			defer logger.SetLogger(baseLogger)
			mockLoader := initLoaderMock(errors.New("loading error"))
			gen := initSymbolsLoadedEventGenerator(mockLoader, nil, nil)

			// First error should always be logged for debug
			event := generateSOLoadedEvent(pid, sharedobjs.ObjInfo{Id: sharedobjs.ObjID{Inode: 1}, Path: "1.so"})
			eventArgs, err := gen.deriveArgs(&event)
			assert.NoError(t, err)
			assert.Nil(t, eventArgs)
			assert.NotEmpty(t, errChan)
			<-errChan
			assert.Empty(t, errChan)

			// Error should be suppressed
			event = generateSOLoadedEvent(pid, sharedobjs.ObjInfo{Id: sharedobjs.ObjID{Inode: 1}, Path: "1.so"})
			eventArgs, err = gen.deriveArgs(&event)
			assert.NoError(t, err)
			assert.Nil(t, eventArgs)
			assert.Empty(t, errChan)
		})
		t.Run("No debug", func(t *testing.T) {
			errChan := setMockLogger(logger.WarnLevel)
			defer logger.SetLogger(baseLogger)
			mockLoader := initLoaderMock(errors.New("loading error"))
			gen := initSymbolsLoadedEventGenerator(mockLoader, nil, nil)

			// First error should create debug log, so we won't see it
			event := generateSOLoadedEvent(pid, sharedobjs.ObjInfo{Id: sharedobjs.ObjID{Inode: 1}, Path: "1.so"})
			eventArgs, err := gen.deriveArgs(&event)
			assert.NoError(t, err)
			assert.Nil(t, eventArgs)
			assert.Empty(t, errChan)

			// Error should be suppressed
			event = generateSOLoadedEvent(pid, sharedobjs.ObjInfo{Id: sharedobjs.ObjID{Inode: 1}, Path: "1.so"})
			eventArgs, err = gen.deriveArgs(&event)
			assert.NoError(t, err)
			assert.Nil(t, eventArgs)
			assert.Empty(t, errChan)
		})
		t.Run("Non-ELF error", func(t *testing.T) {
			errChan := setMockLogger(logger.DebugLevel)
			defer logger.SetLogger(baseLogger)
			mockLoader := initLoaderMock(sharedobjs.InitUnsupportedFileError(nil))
			gen := initSymbolsLoadedEventGenerator(mockLoader, nil, nil)

			// First error should create warning
			event := generateSOLoadedEvent(pid, sharedobjs.ObjInfo{Id: sharedobjs.ObjID{Inode: 1}, Path: "1.so"})
			eventArgs, err := gen.deriveArgs(&event)
			assert.NoError(t, err)
			assert.Nil(t, eventArgs)
			assert.Empty(t, errChan)

			// Error should be suppressed
			event = generateSOLoadedEvent(pid, sharedobjs.ObjInfo{Id: sharedobjs.ObjID{Inode: 1}, Path: "1.so"})
			eventArgs, err = gen.deriveArgs(&event)
			assert.NoError(t, err)
			assert.Nil(t, eventArgs)
			assert.Empty(t, errChan)
		})
	})
}

// setMockLogger set a mock logger as the package logger, and return the output channel of the logger.
func setMockLogger(l logger.Level) <-chan []byte {
	mw, errChan := newMockWriter()
	mockLogger := logger.NewLogger(
		logger.LoggerConfig{
			Writer:  mw,
			Level:   logger.NewAtomicLevelAt(l),
			Encoder: logger.NewJSONEncoder(logger.NewProductionConfig().EncoderConfig),
		},
	)
	logger.SetLogger(mockLogger)
	return errChan
}

type mockWriter struct {
	Out chan<- []byte
}

func newMockWriter() (mockWriter, <-chan []byte) {
	outChan := make(chan []byte, 10)
	writer := mockWriter{Out: outChan}
	return writer, outChan
}

func (w mockWriter) Write(p []byte) (n int, err error) {
	w.Out <- p
	return len(p), nil
}
