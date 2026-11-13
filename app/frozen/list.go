// Package frozen moves everything held by accounts with exposed keys to a
// single destination address, once, during the v11 upgrade.
package frozen

import (
	"bufio"
	_ "embed"
	"fmt"
	"strings"

	_ "github.com/celestiaorg/celestia-app/v11/app/params" // sets the bech32 prefixes
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Destination receives everything moved from the listed accounts.
// TODO: confirm before release.
const Destination = "celestia1atuap9kfnt6y8ykvn3437e7j3z8qffh33pzt53"

//go:embed addresses.txt
var addresses string

// Addresses returns the embedded list of accounts to move.
func Addresses() ([]sdk.AccAddress, error) {
	return ParseList(addresses)
}

// ParseList parses one bech32 account address per line, skipping blank lines
// and # comments. It rejects bad addresses, duplicates and the Destination.
func ParseList(list string) ([]sdk.AccAddress, error) {
	var out []sdk.AccAddress
	seen := make(map[string]struct{})
	s := bufio.NewScanner(strings.NewReader(list))
	for n := 1; s.Scan(); n++ {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		addr, err := sdk.AccAddressFromBech32(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if addr.String() == Destination {
			return nil, fmt.Errorf("line %d: destination %s is listed", n, line)
		}
		if _, ok := seen[string(addr)]; ok {
			return nil, fmt.Errorf("line %d: duplicate address %s", n, line)
		}
		seen[string(addr)] = struct{}{}
		out = append(out, addr)
	}
	return out, s.Err()
}
