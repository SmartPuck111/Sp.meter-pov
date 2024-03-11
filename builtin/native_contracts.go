package builtin

import (
	"encoding/hex"
	"strings"

	"github.com/meterio/meter-pov/abi"
)

func convertABI(abiStr string) *abi.ABI {
	result, _ := abi.New([]byte(abiStr))
	return result
}

func convertBytecode(hexStr string) []byte {
	s, _ := hex.DecodeString(strings.ReplaceAll(hexStr, "0x", ""))
	return s
}

var (
	// this is expanded of locked meter ABI. 03/16/2021
	NewMeterNative_ABI = convertABI(`[{"constant":true,"inputs":[{"name":"addr","type":"address"}],"name":"native_mtr_get","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[{"name":"addr","type":"address"}],"name":"native_master","outputs":[{"name":"","type":"address"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"native_mtr_totalSupply","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"name":"addr","type":"address"},{"name":"amount","type":"uint256"}],"name":"native_mtr_locked_add","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":false,"inputs":[{"name":"addr","type":"address"},{"name":"amount","type":"uint256"}],"name":"native_mtrg_sub","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[{"name":"addr","type":"address"}],"name":"native_mtr_locked_get","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"name":"addr","type":"address"},{"name":"amount","type":"uint256"}],"name":"native_mtr_add","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[{"name":"addr","type":"address"}],"name":"native_mtrg_locked_get","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"native_mtrg_totalSupply","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[{"name":"addr","type":"address"}],"name":"native_mtrg_get","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"name":"addr","type":"address"},{"name":"amount","type":"uint256"}],"name":"native_mtrg_locked_add","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":false,"inputs":[{"name":"addr","type":"address"},{"name":"amount","type":"uint256"}],"name":"native_mtr_locked_sub","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":false,"inputs":[{"name":"addr","type":"address"},{"name":"amount","type":"uint256"}],"name":"native_mtrg_locked_sub","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"native_mtr_totalBurned","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"name":"addr","type":"address"},{"name":"amount","type":"uint256"}],"name":"native_mtrg_add","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"native_mtrg_totalBurned","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"name":"addr","type":"address"},{"name":"amount","type":"uint256"}],"name":"native_mtr_sub","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"inputs":[],"payable":true,"stateMutability":"payable","type":"constructor"},{"anonymous":false,"inputs":[{"indexed":false,"name":"_address","type":"address"},{"indexed":false,"name":"_amount","type":"uint256"},{"indexed":false,"name":"_method","type":"string"}],"name":"MeterTrackerEvent","type":"event"}]`)

	// this is initial syscontract bin file.  appro. 11/18/2020
	// upgrade MeterNative after EdisonSysContract_MainnetStartNum
	NewMeterNative_BinRuntime = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\xec\x96\x6f\x72\xac\x38\x0c\xc4\xaf\xd4\x92\x2c\xc9\x3e\x8e\xff\xde\xff\x08\x5b\xc6\xbc\xb7\x93\x65\x93\x30\x64\x92\x4c\xa5\x02\x35\xf3\xa1\x31\x42\xfa\x59\x34\x32\x44\x18\x02\x94\x0d\x08\x62\x04\x23\x20\x0f\x75\x03\x20\xea\x15\x84\x8b\x47\x02\x82\xc9\xd8\x0f\xb2\x08\x13\x28\x51\x4b\x10\x0a\xf3\x39\x25\xa8\x2f\x35\x25\x45\x4f\x9b\x4a\x28\xbb\xea\xd6\xc5\xa8\x2d\x35\xf6\xa5\x72\x29\x3d\x98\xf7\xa5\x96\xb4\x54\x0f\x28\xa4\x3a\x36\x95\x69\x5f\x9b\x59\xac\x03\x58\xaa\xed\x71\xb3\x20\x0f\xf6\x5d\x4d\xb6\xd4\x5a\xab\xb7\x91\xd6\xd3\xb8\xb7\xa5\xb6\xe1\x9e\xad\x85\x4d\x15\x8a\x4b\xed\x39\x27\x19\x56\x96\x6a\xba\xd4\xf9\xf0\xa0\x43\x97\x9a\xa0\xae\x65\x52\x8c\x18\x4d\x8b\x84\x08\xd2\x59\x75\xc5\xa2\xbb\x74\xdd\x88\x0f\x9d\xfc\x23\xc4\x20\x91\x40\x09\x11\x71\xf2\xff\xcb\xef\xbd\x83\x2c\xc1\xc0\x98\xf7\x26\x4e\x94\xa0\xdb\x39\x73\x19\xaa\x36\x73\x09\x50\x8a\x88\x1c\x69\xee\xf7\xb6\x96\xb6\x35\xfb\x95\xb4\xe5\x3d\xe4\x26\x5b\x22\x3f\x64\x4b\xa1\x7e\x62\xb6\x21\xff\x27\xdb\x7b\xa2\xde\xb3\xf6\x1e\x0a\x29\x1f\x29\xe4\xf9\xba\xa8\x7e\x80\x6d\xd5\x43\x54\x46\x78\x3c\xdb\x79\xff\xab\xbc\x0d\xf2\xb2\x02\xd2\x75\xde\x51\x09\xf3\x91\x0f\x5b\xfa\xea\x4a\xca\x56\x09\x70\x9b\x99\x1f\xfb\x97\xe3\xfc\x77\x2d\x97\x77\x8e\x33\x1f\xa3\x36\xff\xc4\xb7\x22\x22\x5d\xcf\x76\xa4\x43\xb6\x02\x9e\x51\xcb\xf5\xa8\xc2\xe1\x18\xd5\xe4\x8b\xf7\x3c\x99\x1f\xf6\x5c\x9c\x8e\x99\x79\x36\x42\x9e\x46\x7b\xb5\xde\x54\x8f\x51\x5b\xf9\xe2\x7a\x73\x69\x57\xdf\xd6\x99\xbb\x8f\xee\x22\xa3\x95\xe4\xa5\x81\x73\x8f\xd1\x83\x71\x11\x8c\x24\xd1\x54\xba\x67\x07\xd5\x81\x5e\x13\x8f\xca\xb1\x85\xae\xde\x82\x71\x32\x1a\x4c\x21\xce\x49\xe1\x6f\xf4\x28\x9f\xef\xce\xb7\xbb\x14\xf1\xaf\x06\x89\xbc\xcd\x2d\xfd\xf6\xba\x0f\xeb\x46\x1e\x2c\xb9\x99\xea\xb0\xe6\xc1\x59\x87\xb9\xa9\x87\x33\x73\xcb\x8c\xa6\x7f\xf8\x8b\xfe\x61\xff\x82\x67\x26\xdb\x26\x1c\xc5\xbe\x3b\xf6\x83\xf9\xb6\xb7\xf9\x1a\xb9\x78\x30\x75\x3e\x37\x17\x7e\x2f\x5f\x91\x67\xe3\xfb\x62\xed\xab\xfd\xeb\xc1\xe6\x8f\xac\xaa\xb8\x3a\x1c\x56\x3d\x3d\x80\xef\x23\x7b\x57\xa2\x3c\x13\x59\x8c\xf7\xc9\x9a\xeb\x70\x71\xb5\x13\xdd\x7b\x9a\xec\xd6\xb3\xd3\xb9\x75\xef\xdd\x47\xf8\xc2\x74\x86\x27\x62\x7b\xce\x75\xc9\x82\x3d\xd0\x75\xf5\xa7\x7b\x81\x9f\xec\xd8\x93\x6e\xf0\x8d\x5e\xf0\x7c\xdf\xb1\x93\x6e\x70\x76\x52\xf8\xfd\x8e\x7d\xec\x3b\x16\xd8\xd5\xd9\xba\xe9\xff\x3b\xc4\x95\xde\xfd\x81\x3e\x7b\xb6\x6b\x4f\x3a\xed\xaf\xcf\x5e\xf3\xd9\xb7\xbb\xf5\x77\xe6\xba\x6f\x2e\x38\x3b\x71\x5d\x9f\xb9\x30\x55\x35\xf6\xec\xd9\x59\xa0\x91\x11\xd1\x95\x98\x46\x15\x67\xe9\xb9\x37\x6d\xcc\x21\x14\x15\x98\x73\x97\x52\x52\xa4\x0c\x96\x21\xde\xc7\xe8\xb1\x51\xad\x5d\x47\x45\x07\x0b\x05\x80\xd3\x3f\x01\x00\x00\xff\xff\x49\x73\x29\x27\x32\x17\x00\x00")
)