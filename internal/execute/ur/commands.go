package ur

// Universal Router command opcodes (low 7 bits). High bit 0x80 = ALLOW_REVERT.
const (
	CmdV3SwapExactIn       = 0x00
	CmdPermit2TransferFrom = 0x02
	CmdV2SwapExactIn       = 0x08
	CmdWrapETH             = 0x0b
	CmdUnwrapWETH          = 0x0c
)

const urFlagAllowRevert = 0x80

// URAddressThis holds swap output on the router before UNWRAP_WETH.
var URAddressThis = [20]byte{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2,
}

func packCommands(types []byte) []byte {
	out := make([]byte, len(types))
	copy(out, types)
	return out
}
