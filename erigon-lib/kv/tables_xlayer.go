/*
   Copyright 2021 Erigon contributors

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package kv

import (
	"fmt"
)

var ChaindataTables []string

func InitStandaloneSMT(standalone bool) {
	fmt.Printf("[erigon-lib/kv/tables.go] InitStandaloneSMT(%v) called\n", standalone)
	if standalone {
		ChaindataTables = ChaindataTablesInitial
		ChaindataDeprecatedTables = append(ChaindataDeprecatedTablesInitial, TablesSmt...)
	} else {
		ChaindataTables = append(ChaindataTablesInitial, TablesSmt...)
		ChaindataDeprecatedTables = ChaindataDeprecatedTablesInitial
	}
	reinit()
}
