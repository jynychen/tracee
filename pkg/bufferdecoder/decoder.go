// Package bufferdecoder implements simple translation between byte
// sequences and the user-defined structs.
//
// The package favors efficiency over flexibility. The provided API
// allows fast decoding of byte sequence sent by the Tracee eBPF program from
// kernel-space to user-space.
package bufferdecoder

import (
	"encoding/binary"
	"fmt"

	"github.com/aquasecurity/tracee/pkg/errfmt"
	"github.com/aquasecurity/tracee/pkg/events"
	"github.com/aquasecurity/tracee/pkg/events/data"
	"github.com/aquasecurity/tracee/pkg/logger"
	"github.com/aquasecurity/tracee/pkg/time"
	"github.com/aquasecurity/tracee/types/trace"
)

type EbpfDecoder struct {
	buffer      []byte
	cursor      int
	typeDecoder TypeDecoder
}

type ErrBufferTooShort struct {
	expected int
	got      int
	typeName string
}

func (e ErrBufferTooShort) Error() string {
	return fmt.Sprintf("can't read context from buffer (type %s): buffer too short. expected %d, got %d", e.typeName, e.expected, e.got)
}

func (decoder *EbpfDecoder) makeBufferTooShortError(typeName string, expected int) error {
	return ErrBufferTooShort{
		expected: expected,
		got:      len(decoder.buffer[decoder.cursor:]),
		typeName: typeName,
	}
}

// New creates and initializes a new EbpfDecoder using rawBuffer as its initial content.
// The EbpfDecoder takes ownership of rawBuffer, and the caller should not use rawBuffer after this call.
// New is intended to prepare a buffer to read existing data from it, translating it to protocol defined structs.
// The protocol is specific between the Trace eBPF program and the Tracee-eBPF user space application.
func New(rawBuffer []byte, typeDecoder TypeDecoder) *EbpfDecoder {
	return &EbpfDecoder{
		buffer:      rawBuffer,
		cursor:      0,
		typeDecoder: typeDecoder,
	}
}

type presentorFunc func(any) (any, error)
type TypeDecoder []map[string]presentorFunc

func NewTypeDecoder() TypeDecoder {
	typeDecoder := TypeDecoder{
		data.INT_T:  {},
		data.UINT_T: {},
		data.LONG_T: {},
		data.ULONG_T: {
			"time.Time": func(a any) (any, error) {
				argVal, ok := a.(uint64)
				if !ok {
					return nil, errfmt.Errorf("error presenting uint64 as time.Time, type received was %T", a)
				}
				return time.NsSinceEpochToTime(time.BootToEpochNS(argVal)), nil
			},
		},
		data.U16_T:       {},
		data.U8_T:        {},
		data.INT_ARR_2_T: {},
		data.UINT64_ARR_T: {
			"[]trace.HookedSymbolData": func(a any) (any, error) {
				// TODO: this is a temporary solution to present the uint64 array as []trace.HookedSymbolData
				// we need a redesign such that decoders can have access to the kernel symbols table.
				return a, nil
			},
		},
		data.POINTER_T:   {},
		data.BYTES_T:     {},
		data.STR_T:       {},
		data.STR_ARR_T:   {},
		data.SOCK_ADDR_T: {},
		data.CRED_T:      {},
		data.TIMESPEC_T: {
			// timespec is seconds+nano in float
			"float64": func(a any) (any, error) {
				return a, nil
			},
		},
		data.ARGS_ARR_T: {},
		data.BOOL_T:     {},
		data.FLOAT_T:    {},
		data.FLOAT64_T:  {},
	}

	return typeDecoder
}

// BuffLen returns the total length of the buffer owned by decoder.
func (decoder *EbpfDecoder) BuffLen() int {
	return len(decoder.buffer)
}

// BytesRead returns the total amount of bytes that decoder has read from its buffer up until now.
func (decoder *EbpfDecoder) BytesRead() int {
	return decoder.cursor
}

// MoveCursor moves the buffer cursor over n bytes.
// This is useful to skip unwanted data.
// It returns the new cursor position. If the cursor overflows
// (length of buffer is not overflow but end of buffer),
// it will not move and will return the same value.
func (decoder *EbpfDecoder) MoveCursor(n int) int {
	if decoder.cursor+n > len(decoder.buffer) {
		return decoder.cursor
	}
	decoder.cursor += n
	return decoder.cursor
}

// DecodeContext translates data from the decoder buffer, starting from the decoder cursor, to bufferdecoder.EventContext struct.
func (decoder *EbpfDecoder) DecodeContext(eCtx *EventContext) error {
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < eCtx.GetSizeBytes() {
		return errfmt.Errorf("context buffer size [%d] smaller than %d", len(decoder.buffer[offset:]), eCtx.GetSizeBytes())
	}

	// event_context start
	eCtx.Ts = binary.LittleEndian.Uint64(decoder.buffer[offset : offset+8])

	// task_context start
	eCtx.StartTime = binary.LittleEndian.Uint64(decoder.buffer[offset+8 : offset+16])
	eCtx.CgroupID = binary.LittleEndian.Uint64(decoder.buffer[offset+16 : offset+24])
	eCtx.Pid = binary.LittleEndian.Uint32(decoder.buffer[offset+24 : offset+28])
	eCtx.Tid = binary.LittleEndian.Uint32(decoder.buffer[offset+28 : offset+32])
	eCtx.Ppid = binary.LittleEndian.Uint32(decoder.buffer[offset+32 : offset+36])
	eCtx.HostPid = binary.LittleEndian.Uint32(decoder.buffer[offset+36 : offset+40])
	eCtx.HostTid = binary.LittleEndian.Uint32(decoder.buffer[offset+40 : offset+44])
	eCtx.HostPpid = binary.LittleEndian.Uint32(decoder.buffer[offset+44 : offset+48])
	eCtx.Uid = binary.LittleEndian.Uint32(decoder.buffer[offset+48 : offset+52])
	eCtx.MntID = binary.LittleEndian.Uint32(decoder.buffer[offset+52 : offset+56])
	eCtx.PidID = binary.LittleEndian.Uint32(decoder.buffer[offset+56 : offset+60])
	_ = copy(eCtx.Comm[:], decoder.buffer[offset+60:offset+76])
	_ = copy(eCtx.UtsName[:], decoder.buffer[offset+76:offset+92])
	eCtx.Flags = binary.LittleEndian.Uint32(decoder.buffer[offset+92 : offset+96])
	eCtx.LeaderStartTime = binary.LittleEndian.Uint64(decoder.buffer[offset+96 : offset+104])
	eCtx.ParentStartTime = binary.LittleEndian.Uint64(decoder.buffer[offset+104 : offset+112])
	// task_context end

	eCtx.EventID = events.ID(int32(binary.LittleEndian.Uint32(decoder.buffer[offset+112 : offset+116])))
	eCtx.Syscall = int32(binary.LittleEndian.Uint32(decoder.buffer[offset+116 : offset+120]))
	eCtx.Retval = int64(binary.LittleEndian.Uint64(decoder.buffer[offset+120 : offset+128]))
	eCtx.StackID = binary.LittleEndian.Uint32(decoder.buffer[offset+128 : offset+132])
	eCtx.ProcessorId = binary.LittleEndian.Uint16(decoder.buffer[offset+132 : offset+134])
	eCtx.PoliciesVersion = binary.LittleEndian.Uint16(decoder.buffer[offset+134 : offset+136])
	eCtx.MatchedPolicies = binary.LittleEndian.Uint64(decoder.buffer[offset+136 : offset+144])
	// event_context end

	decoder.cursor += eCtx.GetSizeBytes()
	return nil
}

// DecodeArguments decodes the remaining buffer's argument values, according to the given event definition.
// It should be called last, and after decoding the argnum with DecodeUint8.
//
// Argument array passed should be initialized with the size of len(evtFields).
func (decoder *EbpfDecoder) DecodeArguments(args []trace.Argument, argnum int, evtFields []events.DataField, evtName string, eventId events.ID) error {
	for i := 0; i < argnum; i++ {
		idx, arg, err := readArgFromBuff(
			eventId,
			decoder,
			evtFields,
		)
		if err != nil {
			logger.Errorw("error reading argument from buffer", "error", errfmt.Errorf("failed to read argument %d of event %s: %v", i, evtName, err))
			continue
		}
		if args[idx].Value != nil {
			logger.Warnw("argument overridden from buffer", "error", errfmt.Errorf("read more than one instance of argument %s of event %s. Saved value: %v. New value: %v", arg.Name, evtName, args[idx].Value, arg.Value))
		}
		args[idx] = arg
	}

	// Fill missing arguments
	for i := 0; i < len(evtFields); i++ {
		if args[i].Value == nil {
			args[i].ArgMeta = evtFields[i].ArgMeta
			args[i].Value = evtFields[i].Zero
		}
	}
	return nil
}

// DecodeUint8 translates data from the decoder buffer, starting from the decoder cursor, to uint8.
func (decoder *EbpfDecoder) DecodeUint8(msg *uint8) error {
	readAmount := 1
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < readAmount {
		return decoder.makeBufferTooShortError("uint8", readAmount)
	}
	*msg = decoder.buffer[decoder.cursor]
	decoder.cursor += readAmount
	return nil
}

// DecodeInt8 translates data from the decoder buffer, starting from the decoder cursor, to int8.
func (decoder *EbpfDecoder) DecodeInt8(msg *int8) error {
	readAmount := 1
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < readAmount {
		return decoder.makeBufferTooShortError("int8", readAmount)
	}
	*msg = int8(decoder.buffer[offset])
	decoder.cursor += readAmount
	return nil
}

// DecodeUint16 translates data from the decoder buffer, starting from the decoder cursor, to uint16.
func (decoder *EbpfDecoder) DecodeUint16(msg *uint16) error {
	readAmount := 2
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < readAmount {
		return decoder.makeBufferTooShortError("uint16", readAmount)
	}
	*msg = binary.LittleEndian.Uint16(decoder.buffer[offset : offset+readAmount])
	decoder.cursor += readAmount
	return nil
}

// DecodeUint16BigEndian translates data from the decoder buffer, starting from the decoder cursor, to uint16.
func (decoder *EbpfDecoder) DecodeUint16BigEndian(msg *uint16) error {
	readAmount := 2
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < readAmount {
		return decoder.makeBufferTooShortError("uint16", readAmount)
	}
	*msg = binary.BigEndian.Uint16(decoder.buffer[offset : offset+readAmount])
	decoder.cursor += readAmount
	return nil
}

// DecodeInt16 translates data from the decoder buffer, starting from the decoder cursor, to int16.
func (decoder *EbpfDecoder) DecodeInt16(msg *int16) error {
	readAmount := 2
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < readAmount {
		return decoder.makeBufferTooShortError("int16", readAmount)
	}
	*msg = int16(binary.LittleEndian.Uint16(decoder.buffer[offset : offset+readAmount]))
	decoder.cursor += readAmount
	return nil
}

// DecodeUint32 translates data from the decoder buffer, starting from the decoder cursor, to uint32.
func (decoder *EbpfDecoder) DecodeUint32(msg *uint32) error {
	readAmount := 4
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < readAmount {
		return decoder.makeBufferTooShortError("uint32", readAmount)
	}
	*msg = binary.LittleEndian.Uint32(decoder.buffer[offset : offset+readAmount])
	decoder.cursor += readAmount
	return nil
}

// DecodeUint32BigEndian translates data from the decoder buffer, starting from the decoder cursor, to uint32.
func (decoder *EbpfDecoder) DecodeUint32BigEndian(msg *uint32) error {
	readAmount := 4
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < readAmount {
		return decoder.makeBufferTooShortError("uint32 (big endian)", readAmount)
	}
	*msg = binary.BigEndian.Uint32(decoder.buffer[offset : offset+readAmount])
	decoder.cursor += readAmount
	return nil
}

// DecodeInt32 translates data from the decoder buffer, starting from the decoder cursor, to int32.
func (decoder *EbpfDecoder) DecodeInt32(msg *int32) error {
	readAmount := 4
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < readAmount {
		return decoder.makeBufferTooShortError("int32", readAmount)
	}
	*msg = int32(binary.LittleEndian.Uint32(decoder.buffer[offset : offset+readAmount]))
	decoder.cursor += readAmount
	return nil
}

// DecodeUint64 translates data from the decoder buffer, starting from the decoder cursor, to uint64.
func (decoder *EbpfDecoder) DecodeUint64(msg *uint64) error {
	readAmount := 8
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < readAmount {
		return decoder.makeBufferTooShortError("uint64", readAmount)
	}
	*msg = binary.LittleEndian.Uint64(decoder.buffer[offset : offset+readAmount])
	decoder.cursor += readAmount
	return nil
}

// DecodeInt64 translates data from the decoder buffer, starting from the decoder cursor, to int64.
func (decoder *EbpfDecoder) DecodeInt64(msg *int64) error {
	readAmount := 8
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < readAmount {
		return decoder.makeBufferTooShortError("int64", readAmount)
	}
	*msg = int64(binary.LittleEndian.Uint64(decoder.buffer[decoder.cursor : decoder.cursor+readAmount]))
	decoder.cursor += readAmount
	return nil
}

// DecodeBool translates data from the decoder buffer, starting from the decoder cursor, to bool.
func (decoder *EbpfDecoder) DecodeBool(msg *bool) error {
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < 1 {
		return decoder.makeBufferTooShortError("bool", 1)
	}
	*msg = (decoder.buffer[offset] != 0)
	decoder.cursor++
	return nil
}

// DecodeBytes copies from the decoder buffer, starting from the decoder cursor, to msg, size bytes.
func (decoder *EbpfDecoder) DecodeBytes(msg []byte, size int) error {
	offset := decoder.cursor
	bufferLen := len(decoder.buffer[offset:])
	if bufferLen < size {
		return decoder.makeBufferTooShortError("bytes", size)
	}
	_ = copy(msg[:], decoder.buffer[offset:offset+size])
	decoder.cursor += size
	return nil
}

// ReadBytesLen is a helper which allocates a known size bytes buffer and decodes
// the bytes from the buffer into it.
func (decoder *EbpfDecoder) ReadBytesLen(len int) ([]byte, error) {
	var err error
	res := make([]byte, len)
	err = decoder.DecodeBytes(res[:], len)
	if err != nil {
		return nil, errfmt.Errorf("error reading byte array: %v", err)
	}
	return res, nil
}

// DecodeInt32Array translate from the decoder buffer, starting from the decoder cursor, to msg, size * 4 bytes (in order to get int32).
func (decoder *EbpfDecoder) DecodeInt32Array(msg []int32, size int) error {
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < size*4 {
		return decoder.makeBufferTooShortError("[]int32", size*4)
	}
	for i := 0; i < size; i++ {
		msg[i] = int32(binary.LittleEndian.Uint32(decoder.buffer[decoder.cursor : decoder.cursor+4]))
		decoder.cursor += 4
	}
	return nil
}

// DecodeUint64Array translate from the decoder buffer, starting from the decoder cursor, to msg, size * 8 bytes (in order to get int64).
func (decoder *EbpfDecoder) DecodeUint64Array(msg *[]uint64) error {
	var arrLen uint16
	err := decoder.DecodeUint16(&arrLen)
	if err != nil {
		return errfmt.Errorf("error reading ulong array number of elements: %v", err)
	}
	for i := 0; i < int(arrLen); i++ {
		var element uint64
		err := decoder.DecodeUint64(&element)
		if err != nil {
			return errfmt.Errorf("can't read element %d uint64 from buffer: %s", i, err)
		}
		*msg = append(*msg, element)
	}
	return nil
}

// DecodeSlimCred translates data from the decoder buffer, starting from the decoder cursor, to SlimCred struct.
func (decoder *EbpfDecoder) DecodeSlimCred(slimCred *SlimCred) error {
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < 80 {
		return decoder.makeBufferTooShortError("slimCred", 80)
	}
	slimCred.Uid = binary.LittleEndian.Uint32(decoder.buffer[offset : offset+4])
	slimCred.Gid = binary.LittleEndian.Uint32(decoder.buffer[offset+4 : offset+8])
	slimCred.Suid = binary.LittleEndian.Uint32(decoder.buffer[offset+8 : offset+12])
	slimCred.Sgid = binary.LittleEndian.Uint32(decoder.buffer[offset+12 : offset+16])
	slimCred.Euid = binary.LittleEndian.Uint32(decoder.buffer[offset+16 : offset+20])
	slimCred.Egid = binary.LittleEndian.Uint32(decoder.buffer[offset+20 : offset+24])
	slimCred.Fsuid = binary.LittleEndian.Uint32(decoder.buffer[offset+24 : offset+28])
	slimCred.Fsgid = binary.LittleEndian.Uint32(decoder.buffer[offset+28 : offset+32])
	slimCred.UserNamespace = binary.LittleEndian.Uint32(decoder.buffer[offset+32 : offset+36])
	slimCred.SecureBits = binary.LittleEndian.Uint32(decoder.buffer[offset+36 : offset+40])
	slimCred.CapInheritable = binary.LittleEndian.Uint64(decoder.buffer[offset+40 : offset+48])
	slimCred.CapPermitted = binary.LittleEndian.Uint64(decoder.buffer[offset+48 : offset+56])
	slimCred.CapEffective = binary.LittleEndian.Uint64(decoder.buffer[offset+56 : offset+64])
	slimCred.CapBounding = binary.LittleEndian.Uint64(decoder.buffer[offset+64 : offset+72])
	slimCred.CapAmbient = binary.LittleEndian.Uint64(decoder.buffer[offset+72 : offset+80])
	decoder.cursor += int(slimCred.GetSizeBytes())
	return nil
}

// DecodeChunkMeta translates data from the decoder buffer, starting from the decoder cursor, to bufferdecoder.ChunkMeta struct.
func (decoder *EbpfDecoder) DecodeChunkMeta(chunkMeta *ChunkMeta) error {
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < int(chunkMeta.GetSizeBytes()) {
		return decoder.makeBufferTooShortError("chunkMeta", int(chunkMeta.GetSizeBytes()))
	}
	chunkMeta.BinType = BinType(decoder.buffer[offset])
	chunkMeta.CgroupID = binary.LittleEndian.Uint64(decoder.buffer[offset+1 : offset+9])
	_ = copy(chunkMeta.Metadata[:], decoder.buffer[offset+9:offset+37])
	chunkMeta.Size = int32(binary.LittleEndian.Uint32(decoder.buffer[offset+37 : offset+41]))
	chunkMeta.Off = binary.LittleEndian.Uint64(decoder.buffer[offset+41 : offset+49])
	decoder.cursor += int(chunkMeta.GetSizeBytes())
	return nil
}

// DecodeVfsFileMeta translates data from the decoder buffer, starting from the decoder cursor, to bufferdecoder.VfsFileMeta struct.
func (decoder *EbpfDecoder) DecodeVfsFileMeta(vfsFileMeta *VfsFileMeta) error {
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < int(vfsFileMeta.GetSizeBytes()) {
		return decoder.makeBufferTooShortError("vfsFileMeta", int(vfsFileMeta.GetSizeBytes()))
	}
	vfsFileMeta.DevID = binary.LittleEndian.Uint32(decoder.buffer[offset : offset+4])
	vfsFileMeta.Inode = binary.LittleEndian.Uint64(decoder.buffer[offset+4 : offset+12])
	vfsFileMeta.Mode = binary.LittleEndian.Uint32(decoder.buffer[offset+12 : offset+16])
	vfsFileMeta.Pid = binary.LittleEndian.Uint32(decoder.buffer[offset+16 : offset+20])
	decoder.cursor += int(vfsFileMeta.GetSizeBytes())
	return nil
}

// DecodeKernelModuleMeta translates data from the decoder buffer, starting from the decoder cursor, to bufferdecoder.KernelModuleMeta struct.
func (decoder *EbpfDecoder) DecodeKernelModuleMeta(kernelModuleMeta *KernelModuleMeta) error {
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < int(kernelModuleMeta.GetSizeBytes()) {
		return decoder.makeBufferTooShortError("kernelModuleMeta", int(kernelModuleMeta.GetSizeBytes()))
	}
	kernelModuleMeta.DevID = binary.LittleEndian.Uint32(decoder.buffer[offset : offset+4])
	kernelModuleMeta.Inode = binary.LittleEndian.Uint64(decoder.buffer[offset+4 : offset+12])
	kernelModuleMeta.Pid = binary.LittleEndian.Uint32(decoder.buffer[offset+12 : offset+16])
	kernelModuleMeta.Size = binary.LittleEndian.Uint32(decoder.buffer[offset+16 : offset+20])
	decoder.cursor += int(kernelModuleMeta.GetSizeBytes())
	return nil
}

// DecodeBpfObjectMeta translates data from the decoder buffer, starting from the decoder cursor, to bufferdecoder.BpfObjectMeta struct.
func (decoder *EbpfDecoder) DecodeBpfObjectMeta(bpfObjectMeta *BpfObjectMeta) error {
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < int(bpfObjectMeta.GetSizeBytes()) {
		return decoder.makeBufferTooShortError("bpfObjectMeta", int(bpfObjectMeta.GetSizeBytes()))
	}
	_ = copy(bpfObjectMeta.Name[:], decoder.buffer[offset:offset+16])
	bpfObjectMeta.Rand = binary.LittleEndian.Uint32(decoder.buffer[offset+16 : offset+20])
	bpfObjectMeta.Pid = binary.LittleEndian.Uint32(decoder.buffer[offset+20 : offset+24])
	bpfObjectMeta.Size = binary.LittleEndian.Uint32(decoder.buffer[offset+24 : offset+28])
	decoder.cursor += int(bpfObjectMeta.GetSizeBytes())
	return nil
}

// DecodeMprotectWriteMeta translates data from the decoder buffer, starting from the decoder cursor, to bufferdecoder.MprotectWriteMeta struct.
func (decoder *EbpfDecoder) DecodeMprotectWriteMeta(mprotectWriteMeta *MprotectWriteMeta) error {
	offset := decoder.cursor
	if len(decoder.buffer[offset:]) < int(mprotectWriteMeta.GetSizeBytes()) {
		return decoder.makeBufferTooShortError("mprotectWriteMeta", int(mprotectWriteMeta.GetSizeBytes()))
	}
	mprotectWriteMeta.Ts = binary.LittleEndian.Uint64(decoder.buffer[offset : offset+8])
	mprotectWriteMeta.Pid = binary.LittleEndian.Uint32(decoder.buffer[offset+8 : offset+12])

	decoder.cursor += int(mprotectWriteMeta.GetSizeBytes())
	return nil
}

// SetBuffer resets the decoder with a new buffer and resets the cursor to 0.
// This allows reusing decoder instances from a pool.
func (decoder *EbpfDecoder) SetBuffer(newBuffer []byte) {
	decoder.buffer = newBuffer
	decoder.cursor = 0
}
