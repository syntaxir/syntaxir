package bytecode

import "bytes"

// FileMagic begins every compiled grammar file (.sxirc), whose format the File format section of
// spec/bytecode/README.md specifies.
const FileMagic = "SXC\x00"

// IsCompiled reports whether the data begins with FileMagic, that is, whether it is in the
// compiled grammar format rather than a grammar source.
func IsCompiled(data []byte) bool { return bytes.HasPrefix(data, []byte(FileMagic)) }
