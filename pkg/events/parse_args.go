package events

import (
	"bytes"
	"fmt"
	"strconv"
	"unsafe"

	bpf "github.com/aquasecurity/libbpfgo"

	"github.com/aquasecurity/tracee/pkg/errfmt"
	"github.com/aquasecurity/tracee/pkg/events/parsers"
	traceetime "github.com/aquasecurity/tracee/pkg/time"
	"github.com/aquasecurity/tracee/types/trace"
)

func ParseArgs(event *trace.Event) error {
	for i := range event.Args {
		// Convert pointers to hex string
		if ptr, isPointer := event.Args[i].Value.(trace.Pointer); isPointer {
			v := []byte{'0', 'x'}
			v = strconv.AppendUint(v, uint64(ptr), 16)
			event.Args[i].Value = string(v)
		}
	}

	evtID := ID(event.EventID)
	switch evtID {
	case MemProtAlert:
		if alertArg := GetArg(event.Args, "alert"); alertArg != nil {
			if alert, isUint32 := alertArg.Value.(uint32); isUint32 {
				parseMemProtAlert(alertArg, alert)
			}
		}
		if protArg := GetArg(event.Args, "prot"); protArg != nil {
			if prot, isInt32 := protArg.Value.(int32); isInt32 {
				parseMMapProt(protArg, uint64(prot))
			}
		}
		if prevProtArg := GetArg(event.Args, "prev_prot"); prevProtArg != nil {
			if prevProt, isInt32 := prevProtArg.Value.(int32); isInt32 {
				parseMMapProt(prevProtArg, uint64(prevProt))
			}
		}
	case SysEnter, SysExit:
		if syscallArg := GetArg(event.Args, "syscall"); syscallArg != nil {
			if id, isInt32 := syscallArg.Value.(int32); isInt32 {
				parseSyscall(syscallArg, id)
			}
		}
	case CapCapable:
		if capArg := GetArg(event.Args, "cap"); capArg != nil {
			if capability, isInt32 := capArg.Value.(int32); isInt32 {
				parseCapability(capArg, uint64(capability))
			}
		}
	case SecurityMmapFile, DoMmap:
		if protArg := GetArg(event.Args, "prot"); protArg != nil {
			if prot, isUint64 := protArg.Value.(uint64); isUint64 {
				parseMMapProt(protArg, prot)
			}
		}
	case Mmap, Mprotect, PkeyMprotect:
		if protArg := GetArg(event.Args, "prot"); protArg != nil {
			if prot, isInt32 := protArg.Value.(int32); isInt32 {
				parseMMapProt(protArg, uint64(prot))
			}
		}
	case SecurityFileMprotect:
		if protArg := GetArg(event.Args, "prot"); protArg != nil {
			if prot, isInt32 := protArg.Value.(int32); isInt32 {
				parseMMapProt(protArg, uint64(prot))
			}
		}
		if prevProtArg := GetArg(event.Args, "prev_prot"); prevProtArg != nil {
			if prevProt, isInt32 := prevProtArg.Value.(int32); isInt32 {
				parseMMapProt(prevProtArg, uint64(prevProt))
			}
		}
	case Ptrace:
		if reqArg := GetArg(event.Args, "request"); reqArg != nil {
			if req, isInt64 := reqArg.Value.(int64); isInt64 {
				parsePtraceRequestArgument(reqArg, uint64(req))
			}
		}
	case Prctl:
		if optArg := GetArg(event.Args, "option"); optArg != nil {
			if option, isInt32 := optArg.Value.(int32); isInt32 {
				parsePrctlOption(optArg, uint64(option))
			}
		}
	case Socketcall:
		if callArg := GetArg(event.Args, "call"); callArg != nil {
			if call, isInt32 := callArg.Value.(int32); isInt32 {
				parseSocketcallCall(callArg, uint64(call))
			}
		}
	case Socket:
		if domArg := GetArg(event.Args, "domain"); domArg != nil {
			if dom, isInt32 := domArg.Value.(int32); isInt32 {
				parseSocketDomainArgument(domArg, uint64(dom))
			}
		}
		if typeArg := GetArg(event.Args, "type"); typeArg != nil {
			if typ, isInt32 := typeArg.Value.(int32); isInt32 {
				parseSocketType(typeArg, uint64(typ))
			}
		}
	case SecuritySocketCreate, SecuritySocketConnect:
		if domArg := GetArg(event.Args, "family"); domArg != nil {
			if dom, isInt32 := domArg.Value.(int32); isInt32 {
				parseSocketDomainArgument(domArg, uint64(dom))
			}
		}
		if typeArg := GetArg(event.Args, "type"); typeArg != nil {
			if typ, isInt32 := typeArg.Value.(int32); isInt32 {
				parseSocketType(typeArg, uint64(typ))
			}
		}
	case Access:
		if modeArg := GetArg(event.Args, "mode"); modeArg != nil {
			if mode, isInt32 := modeArg.Value.(int32); isInt32 {
				parseAccessMode(modeArg, uint64(mode))
			}
		}
	case Faccessat:
		if modeArg := GetArg(event.Args, "mode"); modeArg != nil {
			if mode, isInt32 := modeArg.Value.(int32); isInt32 {
				parseAccessMode(modeArg, uint64(mode))
			}
		}
		if flagsArg := GetArg(event.Args, "flags"); flagsArg != nil {
			if flags, isInt32 := flagsArg.Value.(int32); isInt32 {
				parseFaccessatFlag(flagsArg, uint64(flags))
			}
		}
	case Execveat:
		if flagsArg := GetArg(event.Args, "flags"); flagsArg != nil {
			if flags, isInt32 := flagsArg.Value.(int32); isInt32 {
				parseExecveatFlag(flagsArg, uint64(flags))
			}
		}
	case Open, Openat, SecurityFileOpen, OpenFileNS, OpenFileMount:
		if flagsArg := GetArg(event.Args, "flags"); flagsArg != nil {
			if flags, isInt32 := flagsArg.Value.(int32); isInt32 {
				parseOpenFlagArgument(flagsArg, uint64(flags))
			}
		}
	case Mknod, Mknodat, SecurityInodeMknod, Chmod, Fchmod, Fchmodat, ChmodCommon:
		if modeArg := GetArg(event.Args, "mode"); modeArg != nil {
			if mode, isUint16 := modeArg.Value.(uint16); isUint16 {
				parseInodeMode(modeArg, uint64(mode))
			}
		}
		if evtID == Fchmodat {
			if flagsArg := GetArg(event.Args, "flags"); flagsArg != nil {
				if flags, isInt32 := flagsArg.Value.(int32); isInt32 {
					parseFchmodatFlag(flagsArg, uint64(flags))
				}
			}
		}
	case Clone:
		if flagsArg := GetArg(event.Args, "flags"); flagsArg != nil {
			if flags, isUint64 := flagsArg.Value.(uint64); isUint64 {
				parseCloneFlags(flagsArg, flags)
			}
		}
	case Bpf, SecurityBPF:
		if cmdArg := GetArg(event.Args, "cmd"); cmdArg != nil {
			if cmd, isInt32 := cmdArg.Value.(int32); isInt32 {
				parseBPFCmd(cmdArg, uint64(cmd))
			}
		}
	case SecurityKernelReadFile, SecurityPostReadFile:
		if typeArg := GetArg(event.Args, "type"); typeArg != nil {
			if readFileId, isInt32 := typeArg.Value.(trace.KernelReadType); isInt32 {
				typeArg.Type = "string"
				typeArg.Value = readFileId.String()
			}
		}
	case SchedProcessExec:
		if modeArg := GetArg(event.Args, "stdin_type"); modeArg != nil {
			if mode, isUint16 := modeArg.Value.(uint16); isUint16 {
				parseInodeMode(modeArg, uint64(mode))
			}
		}
	case DirtyPipeSplice:
		if modeArg := GetArg(event.Args, "in_file_type"); modeArg != nil {
			if mode, isUint16 := modeArg.Value.(uint16); isUint16 {
				parseInodeMode(modeArg, uint64(mode))
			}
		}
	case SecuritySocketSetsockopt, Setsockopt, Getsockopt:
		if levelArg := GetArg(event.Args, "level"); levelArg != nil {
			if level, isInt := levelArg.Value.(int32); isInt {
				parseSocketLevel(levelArg, uint64(level))
			}
		}
		if optionNameArg := GetArg(event.Args, "optname"); optionNameArg != nil {
			if opt, isInt := optionNameArg.Value.(int32); isInt {
				parseGetSocketOption(optionNameArg, uint64(opt), ID(event.EventID))
			}
		}
	case BpfAttach:
		if progTypeArg := GetArg(event.Args, "prog_type"); progTypeArg != nil {
			if progType, isInt := progTypeArg.Value.(int32); isInt {
				parseBPFProgType(progTypeArg, uint64(progType))
			}
		}
		if helpersArg := GetArg(event.Args, "prog_helpers"); helpersArg != nil {
			if helpersList, isUintSlice := helpersArg.Value.([]uint64); isUintSlice {
				parseBpfHelpersUsage(helpersArg, helpersList)
			}
		}
		if attachTypeArg := GetArg(event.Args, "attach_type"); attachTypeArg != nil {
			if attachType, isInt := attachTypeArg.Value.(int32); isInt {
				parseBpfAttachType(attachTypeArg, attachType)
			}
		}
	case SecurityBpfProg:
		if progTypeArg := GetArg(event.Args, "type"); progTypeArg != nil {
			if progType, isInt := progTypeArg.Value.(int32); isInt {
				parseBPFProgType(progTypeArg, uint64(progType))
			}
		}
		if helpersArg := GetArg(event.Args, "helpers"); helpersArg != nil {
			if helpersList, isUintSlice := helpersArg.Value.([]uint64); isUintSlice {
				parseBpfHelpersUsage(helpersArg, helpersList)
			}
		}
	case SecurityPathNotify:
		if maskArg := GetArg(event.Args, "mask"); maskArg != nil {
			if mask, isUint64 := maskArg.Value.(uint64); isUint64 {
				maskArg.Type = "string"
				maskArg.Value = parsers.ParseFsNotifyMask(mask).String()
			}
		}
		if objTypeArg := GetArg(event.Args, "obj_type"); objTypeArg != nil {
			if objType, isUint := objTypeArg.Value.(uint32); isUint {
				parseFsNotifyObjType(objTypeArg, uint64(objType))
			}
		}
	case SuspiciousSyscallSource, StackPivot:
		if vmaFlagsArg := GetArg(event.Args, "vma_flags"); vmaFlagsArg != nil {
			if flags, isUint64 := vmaFlagsArg.Value.(uint64); isUint64 {
				vmaFlagsArg.Type = "string"
				vmaFlagsArg.Value = parsers.ParseVmFlags(flags).String()
			}
		}
	}

	return nil
}

func ParseArgsFDs(event *trace.Event, origTimestamp uint64, fdArgPathMap *bpf.BPFMap) error {
	if fdArg := GetArg(event.Args, "fd"); fdArg != nil {
		if fd, isInt32 := fdArg.Value.(int32); isInt32 {
			ts := origTimestamp
			bs, err := fdArgPathMap.GetValue(unsafe.Pointer(&ts))
			if err != nil {
				return errfmt.WrapError(err)
			}

			fpath := string(bytes.Trim(bs, "\x00"))
			fdArg.Value = fmt.Sprintf("%d=%s", fd, fpath)
		}
	}

	if dirfdArg := GetArg(event.Args, "dirfd"); dirfdArg != nil {
		if dirfd, isInt32 := dirfdArg.Value.(int32); isInt32 {
			parseDirfdAt(dirfdArg, uint64(dirfd))
		}
	}

	return nil
}

func GetArg(args []trace.Argument, argName string) *trace.Argument {
	for i := range args {
		if args[i].Name == argName {
			return &args[i]
		}
	}

	return nil
}

func SetArgValue(event *trace.Event, argName string, value any) error {
	arg := GetArg(event.Args, argName)
	if arg == nil {
		return fmt.Errorf("event %s has no argument named %s", event.EventName, argName)
	}
	arg.Value = value
	return nil
}

// NormalizeTimeArgs normalizes the time arguments of an event, converting them to
// nanoseconds since the epoch.
func NormalizeTimeArgs(args []trace.Argument, timeArgNames []string) error {
	for i := range timeArgNames {
		arg := GetArg(args, timeArgNames[i])
		if arg == nil {
			return errfmt.Errorf("couldn't find argument %s", timeArgNames[i])
		}
		if arg.Value == nil {
			continue
		}

		argTime, ok := arg.Value.(uint64)
		if !ok {
			return errfmt.Errorf("argument %s is not uint64, it is %T",
				timeArgNames[i],
				arg.Value,
			)
		}
		arg.Value = traceetime.BootToEpochNS(argTime)
	}

	return nil
}

type CustomFunctionArgument struct {
	val uint64
	str string
}

func (arg CustomFunctionArgument) String() string {
	return arg.str
}
func (arg CustomFunctionArgument) Value() uint64 {
	return arg.val
}
