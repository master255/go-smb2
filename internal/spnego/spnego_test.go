package spnego

import (
	"bytes"
	"encoding/asn1"
	"encoding/hex"
	"reflect"

	"testing"
)

var testEncodeNegTokenInit = []struct {
	Types    []asn1.ObjectIdentifier
	Token    string
	Expected string
}{
	{
		[]asn1.ObjectIdentifier{NlmpOid},
		"4e544c4d5353500001000000978208e2000000000000000000000000000000000a005a290000000f",
		"604806062b0601050502a03e303ca00e300c060a2b06010401823702020aa22a04284e544c4d5353500001000000978208e2000000000000000000000000000000000a005a290000000f",
	},
}

func TestEncodeNegTokenInit(t *testing.T) {
	for i, e := range testEncodeNegTokenInit {
		tok, err := hex.DecodeString(e.Token)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := hex.DecodeString(e.Expected)
		if err != nil {
			t.Fatal(err)
		}
		ret, err := EncodeNegTokenInit(e.Types, tok)
		if err != nil {
			t.Errorf("%d: %v\n", i, err)
		}
		if !bytes.Equal(ret, expected) {
			t.Errorf("%d: fail\n", i)
		}
	}
}

var testDecodeNegTokenResp = []struct {
	Input                 string
	ExpectedResponseToken string
	Expected              *NegTokenResp
}{
	{
		"a181ca3081c7a0030a0101a10c060a2b06010401823702020aa281b10481ae4e544c4d5353500002000000100010003800000035828962a9d9c92cf4152e98000000000000000066006600480000000601b01d0f000000460041004b004500520055004e00450001001000460041004b004500520055004e00450002001000460041004b004500520055004e00450003001c00660061006b006500720075006e0065002e006c006f00630061006c0004000a006c006f00630061006c00070008000076b91516c2d10100000000",
		"4e544c4d5353500002000000100010003800000035828962a9d9c92cf4152e98000000000000000066006600480000000601b01d0f000000460041004b004500520055004e00450001001000460041004b004500520055004e00450002001000460041004b004500520055004e00450003001c00660061006b006500720075006e0065002e006c006f00630061006c0004000a006c006f00630061006c00070008000076b91516c2d10100000000",
		&NegTokenResp{
			NegState:      1,
			SupportedMech: NlmpOid,
			MechListMIC:   nil,
		},
	},
}

func TestDecodeNegTokenResp(t *testing.T) {
	for i, e := range testDecodeNegTokenResp {
		input, err := hex.DecodeString(e.Input)
		if err != nil {
			t.Fatal(err)
		}
		e.Expected.ResponseToken, err = hex.DecodeString(e.ExpectedResponseToken)
		if err != nil {
			t.Fatal(err)
		}

		ret, err := DecodeNegTokenResp(input)
		if err != nil {
			t.Errorf("%d: %v\n", i, err)
		}
		if !reflect.DeepEqual(ret, e.Expected) {
			t.Errorf("%d: fail, expected %v, got %v\n", i, e.Expected, ret)
		}
	}
}

var testEncodeNegTokenResp = []struct {
	Type        asn1.ObjectIdentifier
	Token       string
	MechListMIC string
	Expected    string
}{
	{
		nil,
		"4e544c4d535350000300000018001800ac0000000e010e01c4000000200020005800000026002600780000000e000e009e00000010001000d2010000158288620a005a290000000f3e3d42661105d1439dee00f836cad4fa4d006900630072006f0073006f00660074004100630063006f0075006e0074006800690072006f00650069006b006f0040006f00750074006c006f006f006b002e006a00700048004f004d0045002d0050004300000000000000000000000000000000000000000000000000bf302e94f761de33288f11866a37b29c01010000000000000076b91516c2d1012753c10d333a7b100000000001001000460041004b004500520055004e00450002001000460041004b004500520055004e00450003001c00660061006b006500720075006e0065002e006c006f00630061006c0004000a006c006f00630061006c00070008000076b91516c2d10106000400020000000800300030000000000000000100000000200000052b42bd2cfdf105bc038de93d80375c47f43366bb9376579cf2e7ffcfd06aaf0a001000000000000000000000000000000000000900200063006900660073002f003100390032002e003100360038002e0030002e003700000000000000000000000000849ee9fcd70ea92c0c4f60e0dfaaf6d2",
		"0100000069e24981b5dac33f00000000",
		"a182020730820203a0030a0101a28201e6048201e24e544c4d535350000300000018001800ac0000000e010e01c4000000200020005800000026002600780000000e000e009e00000010001000d2010000158288620a005a290000000f3e3d42661105d1439dee00f836cad4fa4d006900630072006f0073006f00660074004100630063006f0075006e0074006800690072006f00650069006b006f0040006f00750074006c006f006f006b002e006a00700048004f004d0045002d0050004300000000000000000000000000000000000000000000000000bf302e94f761de33288f11866a37b29c01010000000000000076b91516c2d1012753c10d333a7b100000000001001000460041004b004500520055004e00450002001000460041004b004500520055004e00450003001c00660061006b006500720075006e0065002e006c006f00630061006c0004000a006c006f00630061006c00070008000076b91516c2d10106000400020000000800300030000000000000000100000000200000052b42bd2cfdf105bc038de93d80375c47f43366bb9376579cf2e7ffcfd06aaf0a001000000000000000000000000000000000000900200063006900660073002f003100390032002e003100360038002e0030002e003700000000000000000000000000849ee9fcd70ea92c0c4f60e0dfaaf6d2a31204100100000069e24981b5dac33f00000000",
	},
}

func TestEncodeNegTokenResp(t *testing.T) {
	for i, e := range testEncodeNegTokenResp {
		token, err := hex.DecodeString(e.Token)
		if err != nil {
			t.Fatal(err)
		}
		mechListMIC, err := hex.DecodeString(e.MechListMIC)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := hex.DecodeString(e.Expected)
		if err != nil {
			t.Fatal(err)
		}

		ret, err := EncodeNegTokenResp(e.Type, token, mechListMIC)
		if err != nil {
			t.Errorf("%d: %v\n", i, err)
		}
		if !bytes.Equal(ret, expected) {
			t.Errorf("%d: fail\n", i)
		}
	}
}
