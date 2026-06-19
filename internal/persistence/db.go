package persistence

import (
	"database/sql"
	"net/url"

	_ "github.com/mattn/go-sqlite3"
	"github.com/wzhqwq/VRCDancePreloader/diagnose"
	"github.com/wzhqwq/VRCDancePreloader/internal/persistence/db_vc"
	"github.com/wzhqwq/VRCDancePreloader/internal/utils"
)

var DiagnoseDB *sql.DB

var dataVersion = utils.ShortVersion{
	Major: 1,
	Minor: 0,
}

func InitDiagnoseDB(dbFilePath string) error {
	params := url.Values{}
	params.Add("_journal_mode", "WAL")
	params.Add("_synchronous", "NORMAL")
	params.Add("_temp_store", "MEMORY")

	var err error
	DiagnoseDB, err = sql.Open("sqlite3", dbFilePath+"?"+params.Encode())
	if err != nil {
		return err
	}

	db_vc.Init(DiagnoseDB, dataVersion, diagnose.Tables...)

	diagnose.Init()
	return nil
}

func CloseDiagnoseDB() {
	if DiagnoseDB != nil {
		DiagnoseDB.Close()
	}
}
