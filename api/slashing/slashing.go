// Copyright (c) 2020 The Meter.io developers
// Distributed under the GNU Lesser General Public License v3.0 software license, see the accompanying

// file LICENSE or <https://www.gnu.org/licenses/lgpl-3.0.html>

package slashing

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/meterio/meter-pov/api/utils"
	"github.com/meterio/meter-pov/script/staking"
)

type Slashing struct {
}

func New() *Slashing {
	return &Slashing{}
}

func (sl *Slashing) handleGetInJailList(w http.ResponseWriter, req *http.Request) error {
	list, err := staking.GetLatestInJailList()
	if err != nil {
		return err
	}
	jailedList := convertJailedList(list)
	return utils.WriteJSON(w, jailedList)
}

func (sl *Slashing) handleGetDelegateStatsList(w http.ResponseWriter, req *http.Request) error {
	list, err := staking.GetLatestDelegateStatList()
	if err != nil {
		return err
	}
	statsList := convertDelegateStatList(list)
	return utils.WriteJSON(w, statsList)
}

func (sl *Slashing) Mount(root *mux.Router, pathPrefix string) {
	sub := root.PathPrefix(pathPrefix).Subrouter()
	sub.Path("/injail").Methods("Get").HandlerFunc(utils.WrapHandlerFunc(sl.handleGetInJailList))
	sub.Path("/statistics").Methods("Get").HandlerFunc(utils.WrapHandlerFunc(sl.handleGetDelegateStatsList))

}
