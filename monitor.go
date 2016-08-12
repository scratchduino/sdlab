/*
    sdlab - ScratchDuino Laboratory core daemon
    Copyright (C) 2014  Dmitry Mikhirev <mikhirev@mezon.ru>

    This program is free software: you can redistribute it and/or modify
    it under the terms of the GNU General Public License as published by
    the Free Software Foundation, either version 3 of the License, or
    (at your option) any later version.

    This program is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU General Public License for more details.

    You should have received a copy of the GNU General Public License
    along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package main

import (
	"github.com/pborman/uuid"
	"errors"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"database/sql"
	"strconv"
	"time"
	"strings"
)

const (
	RFC3339_UTC      = "2006-01-02T15:04:05Z"
	RFC3339NANO3_UTC = "2006-01-02T15:04:05.999Z"
)

type MonValue struct {
	Name     string
	Sensor   string
	ValueIdx int
	Type     ValueType    // TODO: remove Type using

	previous float64
}

type Monitor struct {
	Id       int
	UUID     uuid.UUID
	Exp_id   int
	Setup_id int
	Step     uint         // Interval
	Remind   uint         // Amount decremented
	Created  time.Time
	StopAt   time.Time
	Active   bool

	stop     chan int

	Values   []MonValue
}

type MonitorDBItem struct {
	Id       int
	UUID     string
	Exp_id   int
	Setup_id int
	Step     uint         // Interval
	Remind   uint         // Amount decremented
	Created  string
	StopAt   string
	Active   bool

	Values   []MonValue
}

type DetectionItem struct {
	Id            int
	Exp_id        int
	Mon_id        int
	Time          time.Time
	Sensor_id     string
	Sensor_val_id int
	Detection     float64
	Error         string    // TODO: remode old error field
}

type DetectionDBItem struct {
	Id            int
	Exp_id        int
	Mon_id        int
	Time          string
	Sensor_id     string
	Sensor_val_id int
	Detection     float64
	Error         string    // TODO: remode old error field
}

type MonValueInfo struct {
	Name     string
	Sensor   string
	ValueIdx int
	Len      uint
}

type ArchiveInfo struct {
	Step uint
	Len  uint
}

type MonitorInfo struct {
	Created  time.Time
	StopAt   time.Time
	Last     time.Time
	Archives []ArchiveInfo
	Values   []MonValueInfo
}

type FetchResultDBItem struct {
	Time          time.Time
	Name          string
	Detection     float64
	Error         string    // TODO: remode old error field
}

type FetchResultDB struct {
	Filename string
	Cf       string
	Start    time.Time
	End      time.Time
	Step     time.Duration
	DsNames  []string
	RowCnt   int
	DsData   []*FetchResultDBItem
	// contains filtered or unexported fields
}

var (
	db       *sql.DB
	queries  map[string]string
	stmts    map[string]*sql.Stmt
	monitors map[string]*Monitor
)

func initQueries(dbtype string) error {
	var err error

	// Prepare plain queries
	if queries == nil {
		queries = make(map[string]string)
	}

	// Database specific queries
	// - pre: prerequisite configuration, database fixes and etc.
	switch dbtype {
	case "sqlite":
		queries["_pre"] = `
			PRAGMA automatic_index = ON;
			PRAGMA busy_timeout = 50000000;
			PRAGMA cache_size = 32768;
			PRAGMA cache_spill = OFF;
			PRAGMA foreign_keys = OFF;
			PRAGMA journal_mode = WAL;
			PRAGMA journal_size_limit = 67110000;
			PRAGMA locking_mode = NORMAL;
			PRAGMA page_size = 4096;
			PRAGMA recursive_triggers = ON;
			PRAGMA secure_delete = ON;
			PRAGMA synchronous = NORMAL;
			PRAGMA temp_store = MEMORY;
			PRAGMA wal_autocheckpoint = 16384;
		`
		/*
		queries["_pre"] = `
			PRAGMA busy_timeout = 50000000;
		`*/

		/*
		Sqlite Database PRAGMAs
		@see http://www.sqlite.org/pragma.html
			automatic_index    - ? (default enabled)
			busy_timeout       - sleeps for a specified amount of time when a table is locked (default 0)
			cache_size         - maximum number of database disk pages that SQLite will hold in memory at once 
								 per open database file. default "-2000" (cache size is limited to 2048000 bytes)
			cache_spill        - enables or disables the ability of the pager to spill dirty cache pages to 
								 the database file in the middle of a transaction (default enabled).
			foreign_keys       - default OFF
			journal_mode       - (WAL journaling mode uses a write-ahead log instead of a rollback journal 
								 to implement transactions)
			journal_size_limit - limit the size of rollback-journal and WAL files left in the file-system 
								 after transactions or checkpoints 
								 (the write-ahead log file is not truncated following a checkpoint)
			locking_mode       - the database connection locking-mode.
								 The locking-mode is either NORMAL or EXCLUSIVE (default NORMAL).
								 NORMAL - a database connection unlocks the database file at the conclusion of 
								 each read or write transaction
			page_size          - (default 4096)
			recursive_triggers - (default 0)
			secure_delete      - When secure-delete on, SQLite overwrites deleted content with zeros. (default 1)
			synchronous        - NORMAL - the SQLite database engine will still sync at the most critical moments, 
								 but less often than in FULL mode (default FULL)
			temp_store         - When temp_store is MEMORY temporary tables and indices are kept in as 
								 if they were pure in-memory databases memory. (default DEFAULT)
			wal_autocheckpoint - This pragma queries or sets the write-ahead log auto-checkpoint interval. 
								 (default enabled 1000)
		*/
	case "mysql":
			// TODO: mysql pragma?
			fallthrough
	default: 
		queries["_pre"] = ``
	}

	// TABLE: monitors
	queries["monitors_select_all"] = `
		SELECT *
		FROM monitors
		ORDER BY id;
	`
	queries["monitors_select_all_id"] = `
		SELECT id
		FROM monitors
		ORDER BY id;
	`
	queries["monitors_select_by_id"] = `
		SELECT *
		FROM monitors
		WHERE id = ? ;
	`
	queries["monitors_count"] = `
		SELECT COUNT(*)
		FROM monitors;
	`
	queries["monitors_insert"] = `
		INSERT INTO monitors (uuid, exp_id, setup_id, interval, remind, created, stopat)
		VALUES (?, ?, ?, ?, ?, ?, ?);
	`
	queries["monitors_replace"] = `
		INSERT OR REPLACE INTO monitors (id, uuid, exp_id, setup_id, interval, remind, created, stopat)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?);
	`
	queries["monitors_delete_by_id"] = `
		DELETE FROM monitors
		WHERE id = ?;
	`

	// TABLE: monitors_values
	queries["monitors_values_select_by_uuid"] = `
		SELECT *
		FROM monitors_values
		WHERE uuid = ?;
	`
	queries["_monitors_values_replace_into"] = `INSERT OR REPLACE INTO monitors_values(uuid, name, sensor, valueidx)`
	queries["_monitors_values_replace_values"] = `(?, ?, ?, ?)`
	queries["monitors_values_delete_by_uuid"] = `
		DELETE FROM monitors_values
		WHERE uuid = ?;
	`

	// TABLE: detections
	queries["detections_select_by_monitor_time_range"] = `
		SELECT time, sensor_id, sensor_val_id, detection, error
		FROM detections
		WHERE (mon_id = ?) AND (time BETWEEN ? AND ?)
		ORDER BY strftime('%s', time), strftime('%f', time), sensor_id, sensor_val_id;
	`
	queries["detections_count_by_monitor"] = `
		SELECT COUNT(*)
		FROM detections
		WHERE mon_id = ?;
	`
	queries["detections_count_by_monitor_grouptime"] = `
		SELECT COUNT(*)
		FROM (
			SELECT time
			FROM detections
			WHERE mon_id = ?
			GROUP BY time
		);
	`
	queries["detections_count_by_monitor_sensor_grouptime"] = `
		SELECT time
		FROM detections
		WHERE mon_id = ? AND sensor_id = ? AND sensor_val_id = ?
		ORDER BY strftime('%s', time) DESC, strftime('%f', time) DESC
		LIMIT 1;
	`
	queries["detections_select_last_time_by_monitor"] = `
		SELECT time
		FROM detections
		WHERE mon_id = ?
		ORDER BY strftime('%s', time) DESC, strftime('%f', time) DESC
		LIMIT 1;
	`
	queries["detections_insert"] = `
		INSERT INTO detections(exp_id, mon_id, time, sensor_id, sensor_val_id, detection, error)
		VALUES (?, ?, ?, ?, ?, ?, ?);
	`
	queries["_detections_insert_into"] = `INSERT INTO detections(exp_id, mon_id, time, sensor_id, sensor_val_id, detection, error)`
	queries["_detections_insert_values"] = `(?, ?, ?, ?, ?, ?, ?)`
	queries["detections_delete_by_monitor"] = `
		DELETE FROM detections
		WHERE mon_id = ?;
	`

	// Prepare statements
	stmts = make(map[string]*sql.Stmt)

	for qname, value := range queries {
		if string([]rune(qname)[0]) == "_" {
			continue
		}
		stmts[qname], err = db.Prepare(value)
		if err != nil {
			return err
		}
	}

	return nil
}

func cleanupQueries() {
	for _, stmt := range stmts {
		if stmt != nil {
			stmt.Close()
		}
	}
	// TODO: return error on Close
}

func prepareDB() error {
	if pre, ok := queries["_pre"]; !ok || (pre == "") {
		return nil
	}

	_, err := db.Exec(queries["_pre"])
	if err != nil {
		return err
	}

	return nil
}

func monitorToDB(mon *Monitor) (monDBi *MonitorDBItem, err error) {
	uuid := mon.UUID.String()
	monDBi = &MonitorDBItem{
		mon.Id,
		uuid,
		mon.Exp_id,
		mon.Setup_id,
		mon.Step,
		mon.Remind,
		mon.Created.Format(time.RFC3339Nano),
		mon.StopAt.Format(time.RFC3339Nano),
		mon.Active,

		mon.Values,
	}
	return monDBi, nil
}

func monitorFromDB(mondbi *MonitorDBItem) (mon *Monitor, err error) {
	uuid := uuid.Parse(mondbi.UUID)
	created, err := time.Parse(time.RFC3339Nano, mondbi.Created)
	if err != nil {
		return nil, err
	}
	stopAt, err := time.Parse(time.RFC3339Nano, mondbi.StopAt)
	if err != nil {
		return nil, err
	}
	/*
	exp_id := 0
	if len(string(mondbi.Exp_id)) > 0 {
		exp_id, _ = strconv.Atoi(mondbi.Exp_id)
	}
	setup_id := 0
	if len(string(mondbi.Setup_id)) > 0 {
		setup_id, _ = strconv.Atoi(mondbi.Setup_id)
	}
	step := 0
	if len(string(mondbi.Step)) > 0 {
		step, _ = strconv.Atoi(mondbi.Step)
	}
	remind := 0
	if len(string(mondbi.Remind)) > 0 {
		remind, _ = strconv.Atoi(mondbi.Remind)
	}
	for i, v := range mondbi.Values {
		mondbi.Values[i] = mondbi.Values[i]
	}
	*/
	mon = &Monitor{
		mondbi.Id,
		uuid,
		mondbi.Exp_id,
		mondbi.Setup_id,
		mondbi.Step,
		mondbi.Remind,
		created,
		stopAt,
		mondbi.Active,
		nil,
		mondbi.Values,
	}
	return mon, nil
}

// loadMonitor reads database item and creates a new Monitor
// object.
func loadMonitor(tx *sql.Tx, monid int) (*Monitor, error) {
	var err, err2 error

	mondbi := MonitorDBItem{}
	mondbi.Values = make([]MonValue, 0)

	// Load Monitor
	row := tx.Stmt(stmts["monitors_select_by_id"]).QueryRow(monid)
	err = row.Scan(
		&mondbi.Id,
		&mondbi.UUID,
		&mondbi.Exp_id,
		&mondbi.Setup_id,
		&mondbi.Step,
		&mondbi.Remind,
		&mondbi.Created,
		&mondbi.StopAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			// there were no rows, but otherwise no error occurred
			return nil, err
		} else {
			//fmt.Printf(LPURPLE+"loadMonitor#%05d:"+RED+" Fatal Monitor Select Stmt QueryRow %s, continue\n"+NCO, monid, err)
			logger.Print("Fatal Monitor Select Stmt QueryRow: " + err.Error())
			err2 = tx.Rollback()
			if err2 != nil {
				//fmt.Printf(LPURPLE+"loadMonitor#%05d:"+RED+" Fatal Monitor Select Stmt Rollback %s, continue\n"+NCO, monid, err2)
				logger.Print("Fatal Monitor Select Stmt Rollback: " + err2.Error())
				return nil, err2
			}
			return nil, err
		}
	}

	// Load Monitor Values
	rows, err := tx.Stmt(stmts["monitors_values_select_by_uuid"]).Query(mondbi.UUID)
	if err != nil {
		//fmt.Printf(LPURPLE+"loadMonitor#%05d:"+RED+" Fatal Monitor UUID %s Values Stmt Query %s, continue\n"+NCO, monid, mondbi.UUID, err)
		logger.Print("Fatal Monitor UUID %s Values Stmt Query: ", mondbi.UUID, err.Error())
		err2 = tx.Rollback()
		if err2 != nil {
			//fmt.Printf(LPURPLE+"loadMonitor#%05d:"+RED+" Fatal Monitor UUID %s Values Stmt Rollback %s, continue\n"+NCO, monid, mondbi.UUID, err2)
			logger.Print("Fatal Monitor UUID %s Values Stmt Rollback: ", mondbi.UUID, err2.Error())
			return nil, err2
		}
		return nil, err2
	}
	defer rows.Close()

	for rows.Next() {

		monv := new(MonValue)
		monuuid := ""

		err = rows.Scan(&monuuid, &monv.Name, &monv.Sensor, &monv.ValueIdx)
		if err != nil {
			//fmt.Printf(LPURPLE+"loadMonitor#%05d:"+RED+" Fatal Scan Monitor UUID %s Values %s, continue\n"+NCO, monid, mondbi.UUID, err)
			logger.Printf("Fatal Scan Monitor UUID %s Values: %s", mondbi.UUID, err.Error())
			err2 = tx.Rollback()
			if err2 != nil {
				//fmt.Printf(LPURPLE+"loadMonitor#%05d:"+RED+" Fatal Scan Monitor UUID %s Values Rollback %s, continue\n"+NCO, monid, mondbi.UUID, err2)
				logger.Printf("Fatal Scan Monitor UUID %s Values Rollback: ", mondbi.UUID, err2.Error())
			}
			continue
		}

		mondbi.Values = append(mondbi.Values, *monv)
	}

	// Convert Monitor from DB 
	mon, err := monitorFromDB(&mondbi)
	if err == nil && mon.Active {
		err = mon.Run()
	}

	return mon, err
}

// loadRunMonitors looks for saved monitors, loads them and run those having
// state active.
func loadRunMonitors() error {
	var err, err2 error
	var monid int = 0

	logger.Print("Loading monitors...")

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	// Count monitors
	row := tx.Stmt(stmts["monitors_count"]).QueryRow()
	var count int64 = 0
	err = row.Scan(&count)
	if err != nil {
		if err == sql.ErrNoRows {
			// there were no rows, but otherwise no error occurred
			count = 0
		} else {
			//fmt.Printf(LPURPLE+"loadRunMonitors:"+RED+" Fatal Monitor Count Stmt %s, exiting\n"+NCO, err)
			logger.Print("Fatal Monitor Count Stmt Query: " + err.Error())
			err2 = tx.Rollback()
			if err2 != nil {
				//fmt.Printf(LPURPLE+"loadRunMonitors:"+RED+" Fatal Monitor Count Stmt Rollback %s, exiting\n"+NCO, err2)
				logger.Print("Fatal Monitor Count Stmt Rollback: " + err2.Error())
				return err2
			}
			return err
		}
	}

	monitors = make(map[string]*Monitor, count)

	// Load monitors
	rows, err := tx.Stmt(stmts["monitors_select_all_id"]).Query()
	if err != nil {
		//fmt.Printf(LPURPLE+"loadRunMonitors:"+RED+" Fatal Monitor All Ids Stmt Query %s, exiting\n"+NCO, err)
		logger.Print("Fatal Monitor All Ids Stmt Query: " + err.Error())
		err2 = tx.Rollback()
		if err2 != nil {
			//fmt.Printf(LPURPLE+"loadRunMonitors:"+RED+" Fatal Stmt Rollback %s, exiting\n"+NCO, err2)
			logger.Print("Fatal Monitor All Ids Stmt Rollback: " + err2.Error())
			return err2
		}
		return err
	}
	defer rows.Close()

	uuids := make([]string, 0)  // DEBUG

	for rows.Next() {
		monid = 0

		err = rows.Scan(&monid)
		if err != nil {
			//fmt.Printf(LPURPLE+"loadRunMonitors:"+RED+" Fatal Scan Monitor Id %s, exiting\n"+NCO, err)
			logger.Printf("Fatal Scan Monitor Id: %s", err.Error())
			err2 = tx.Rollback()
			if err2 != nil {
				//fmt.Printf(LPURPLE+"loadRunMonitors:"+RED+" Fatal Scan Monitor Id Rollback %s, exiting\n"+NCO, err2)
				logger.Printf("Fatal Scan Monitor Id Rollback: %s", err2.Error())
			}
			continue
		}

		if monid == 0 {
			continue
		}

		mon, err := loadMonitor(tx, monid)
		if err != nil {
			logger.Print(err)
			continue
		}

		if mon.Active {
			if mon.StopAt.IsZero() || mon.StopAt.After(time.Now()) {
				err = mon.Run()
			} else {
				mon.Active = false
				err = mon.Save()
			}
			if err != nil {
				logger.Print(err)
			}
		}
		monitors[string(mon.UUID)] = mon
		
		uuids = append(uuids, string(mon.UUID))
	}
	//fmt.Printf(LPURPLE+"loadRunMonitors#%-23s:"+NCO+" Count Monitors %d Rows %s\n", time.Now().Format(RFC3339NANO3_UTC), count, strings.Join(uuids, ", "))
	logger.Printf("Found %d monitors: [%s]\n", count, strings.Join(uuids, ", "))

	// CLOSE ROWS HERE!???
	//rows.Close()

	err = tx.Commit()
	if err != nil {
		//fmt.Printf(LPURPLE+"loadRunMonitors:"+RED+" Fatal Commit Load Monitors %s, exiting\n"+NCO, err)
		logger.Printf("Fatal Commit Load Monitors %s, exiting\n", err.Error())
		return err
	}

	return nil
}

// initDB(config.Database) initialize database instance, loads them and run those having
// state active.
func initDB(dbconf DatabaseConf) (*sql.DB, error) {
	var dbo *sql.DB
	var err error

	logger.Print("Connect database...")

	switch dbconf.Type {
	case "sqlite":
		dbo, err = sql.Open("sqlite3", dbconf.Dsn)
		if err != nil {
			return nil, err
		}
		if dbo == nil {
			err = errors.New("Database in nil")
			if err != nil {
				return nil, err
			}
		}

		// TODO: Check connection (cannot use Ping() with sqlite, cannot test file exists instead of DSN string params)

	case "mysql":
		// Todo: add instantiate mysql database

		/*
		// Check connection
		err = dbo.Ping()
		if err != nil {
			return nil, err
		}
		*/

		fallthrough

	default:
		err = errors.New("Unknown database type")
		if err != nil {
			return dbo, err
		}
	}

	return dbo, err
}

func (mon *Monitor) Run() error {
	d := time.Duration(mon.Step) * time.Second
	t := time.NewTicker(d)
	readings := make([](chan float64), len(mon.Values))
	for i := range readings {
		readings[i] = make(chan float64, 1)
	}
	mon.stop = make(chan int, 1)
	vals := make([]interface{}, len(mon.Values)+1)
	go func() {
		for tm := range t.C {
			if (!mon.StopAt.IsZero()) && mon.StopAt.Before(tm) {
				mon.Stop()
			}
			if len(mon.stop) > 0 {
				return
			}
			for i, v := range mon.Values {
				go getSerData(v.Sensor, v.ValueIdx, readings[i])
			}
			vals[0] = tm
			for i, c := range readings {
				vals[i+1] = <-c
				mon.Values[i].previous = vals[i+1].(float64)
			}
			mon.Update(vals...)
		}
	}()
	return nil
}

func (mon *Monitor) Update(vals ...interface{}) error {
	var err error

	if len(vals) < 2 {
		/*
		errf := fmt.Errorf("Update Error: no new detections for %s", mon.UUID.String())
		if errf != nil {
			return errf
		}
		*/
		return nil
	}

	// TODO: Drop bad Sensor values : min-max check?
	var nulltime time.Time
	tm, ok := vals[0].(time.Time)
	if !ok {
		tm = nulltime
	}

	det := DetectionDBItem{
		Id:             0,
		Exp_id:         mon.Exp_id,
		Mon_id:         mon.Id,
		Time:           tm.Format(RFC3339NANO3_UTC),
		Sensor_id:      "",
		Sensor_val_id:  0,
		Detection:      0,
		Error:          "",  // TODO: remode old error field
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	sqlInsert := queries["_detections_insert_into"] + " VALUES "
	values := []interface{}{}
	for i, v := range vals {
		if i == 0 {
			// Skip time
			continue
		}

		sqlInsert += queries["_detections_insert_values"] + ","

		values = append(values,
			det.Exp_id,
			det.Mon_id,
			det.Time,
			mon.Values[i-1].Sensor,
			mon.Values[i-1].ValueIdx,
			v,
			"",
		)
	}
	sqlInsert = strings.TrimSuffix(sqlInsert, ",")

	// Prepare the statement
	stmt, err := tx.Prepare(sqlInsert)
	if err != nil {
		return err
	}

	// Execute
	//res, err := stmt.Exec(values...)
	_, err = stmt.Exec(values...)
	if err != nil {
		return err
	}

	//logger.Printf("Update: Inserted for Monitor %s Count Detections %d", mon.Id, res.RowsAffected())

	err = tx.Commit()
	if err != nil {
		//fmt.Printf(LBLUE+"Update:"+RED+" Fatal Commit Update Detections for %s\n"+NCO, mon.UUID.String(), err)
		//logger.Printf("Update: Fatal Commit Update Detections for %s: %s", mon.UUID.String(), err.Error())
		return err
	}

	//fmt.Printf(LBLUE+"Update %-23s:"+NCO+" insert detections\n", time.Now().Format("2006-01-02T15:04:05.999"))
	//logger.Printf("Update %-23s: insert detections for %s", time.Now().Format("2006-01-02T15:04:05.999"), mon.UUID.String())

	return nil
}

func (mon *Monitor) Stop() error {
	if !mon.Active {
		return errors.New("Monitor " + mon.UUID.String() + " is inactive")
	}
	mon.stop <- 1
	mon.Active = false
	return mon.Save()
}

func (mon *Monitor) Save() error {
	var err error

	monDBi, err := monitorToDB(mon)
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	// Insert / Update monitor
	if monDBi.Id > 0 {
		_, err = tx.Stmt(stmts["monitors_replace"]).Exec(
			monDBi.Id,
			monDBi.UUID,
			monDBi.Exp_id,
			monDBi.Setup_id,
			monDBi.Step,
			monDBi.Remind,
			monDBi.Created,
			monDBi.StopAt,
			monDBi.Active,
		)
	} else {
		_, err = tx.Stmt(stmts["monitors_insert"]).Exec(
			monDBi.UUID,
			monDBi.Exp_id,
			monDBi.Setup_id,
			monDBi.Step,
			monDBi.Remind,
			monDBi.Created,
			monDBi.StopAt,
			monDBi.Active,
		)
	}
	if err != nil {
		return err
	}

	// Save Monitor Values
	sqlInsert := queries["_monitors_values_replace_into"] + " VALUES "
	values := []interface{}{}
	for _, monv := range monDBi.Values {
		sqlInsert += queries["_monitors_values_replace_values"] + ","
		values = append(values,
			monDBi.UUID,
			monv.Name,
			monv.Sensor,
			monv.ValueIdx,
		)
	}
	sqlInsert = strings.TrimSuffix(sqlInsert, ",")

	// Prepare the statement
	stmt, err := tx.Prepare(sqlInsert)
	if err != nil {
		return err
	}

	// Execute
	//res, err := stmt.Exec(values...)
	_, err = stmt.Exec(values...)
	if err != nil {
		return err
	}

	//logger.Printf("Save: Inserted for Monitor %s Count Values %d", monDBi.UUID, res.RowsAffected())

	err = tx.Commit()
	if err != nil {
		//fmt.Printf(LBLUE+"Save:"+RED+" Fatal Commit Save Monitor %s\n"+NCO, monDBi.UUID, err)
		//logger.Printf("Fatal Commit Save Monitor %s: %s", monDBi.UUID, err.Error())
		return err
	}

	//fmt.Printf(LBLUE+"Save %-23s:"+NCO+" update monitor %s\n", time.Now().Format("2006-01-02T15:04:05.999"), monDBi.UUID)
	//logger.Printf("Save %-23s: update monitor %s", time.Now().Format("2006-01-02T15:04:05.999"), monDBi.UUID)

	return nil
}

func (mon *Monitor) Info() (*MonitorInfo, error) {
	var err, err2 error

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}

	// Count grouped detections
	row := tx.Stmt(stmts["detections_count_by_monitor_grouptime"]).QueryRow(mon.Id)
	var alen uint = 0
	err = row.Scan(&alen)
	if err != nil {
		if err == sql.ErrNoRows {
			// there were no rows, but otherwise no error occurred
			alen = 0
		} else {
			//fmt.Printf(LPURPLE+"Info:"+RED+" Fatal Detections Grouped Count Stmt %s, exiting\n"+NCO, err)
			logger.Print("Fatal Detections Grouped Count Stmt Query: " + err.Error())
			err2 = tx.Rollback()
			if err2 != nil {
				//fmt.Printf(LPURPLE+"Info:"+RED+" Fatal Detections Grouped Count Stmt Rollback %s, exiting\n"+NCO, err2)
				logger.Print("Fatal Detections Grouped Count Stmt Rollback: " + err2.Error())
				return nil, err2
			}
			return nil, err
		}
	}

	// Get last detection time
	row = tx.Stmt(stmts["detections_select_last_time_by_monitor"]).QueryRow(mon.Id)
	var nulltime time.Time
	lasttxt, last := "", nulltime
	err = row.Scan(&lasttxt)
	if err != nil {
		if err == sql.ErrNoRows {
			// there were no rows, but otherwise no error occurred
			lasttxt = ""
		} else {
			//fmt.Printf(LPURPLE+"Info:"+RED+" Fatal Detections Last Time Stmt %s, exiting\n"+NCO, err)
			logger.Print("Fatal Detections Last Time Stmt Query: " + err.Error())
			err2 = tx.Rollback()
			if err2 != nil {
				//fmt.Printf(LPURPLE+"Info:"+RED+" Fatal Detections Last Time Stmt Rollback %s, exiting\n"+NCO, err2)
				logger.Print("Fatal Detections Last Time Stmt Rollback: " + err2.Error())
				return nil, err2
			}
			return nil, err
		}
	}
	if lasttxt != "" {
		last, _ = time.Parse(time.RFC3339Nano, lasttxt)
	}

	n := 1  // number of archives, only one archive now, no step multiplied stores on Step*2, Step*4, Step*16, and etc.
	ai := make([]ArchiveInfo, n)
	for i := range ai {
		ai[i] = ArchiveInfo{
			mon.Step, // archive data step
			alen,
		}
	}

	// Get Values data
	var vlen uint
	vi := make([]MonValueInfo, len(mon.Values))
	for i := range vi {
		// Count separate Values
		vlen = 0
		row = tx.Stmt(stmts["detections_count_by_monitor_sensor_grouptime"]).QueryRow(
			mon.Id,
			mon.Values[i].Sensor,
			mon.Values[i].ValueIdx,
		)
		err = row.Scan(&vlen)
		if err != nil {
			if err == sql.ErrNoRows {
				// there were no rows, but otherwise no error occurred
				vlen = 0
			} else {
				//fmt.Printf(LPURPLE+"Info:"+RED+" Fatal Detections Grouped Sensor Count Stmt %s, exiting\n"+NCO, err)
				logger.Print("Fatal Detections Grouped Sensor Count Stmt Query: " + err.Error())
				err2 = tx.Rollback()
				if err2 != nil {
					//fmt.Printf(LPURPLE+"Info:"+RED+" Fatal Detections Grouped Sensor Count Stmt Rollback %s, exiting\n"+NCO, err2)
					logger.Print("Fatal Detections Grouped Sensor Count Stmt Rollback: " + err2.Error())
					return nil, err2
				}
				return nil, err
			}
		}

		vi[i] = MonValueInfo{
			mon.Values[i].Name,
			mon.Values[i].Sensor,
			mon.Values[i].ValueIdx,
			vlen,
		}
	}

	err = tx.Commit()
	if err != nil {
		//fmt.Printf(LPURPLE+"Info:"+RED+" Fatal Commit Detections Grouped Sensor Count %s, exiting\n"+NCO, err)
		logger.Printf("Fatal Commit Monitor Info %s, exiting\n", err.Error())
		return nil, err
	}

	mi := &MonitorInfo{
		mon.Created,
		mon.StopAt,
		last,
		ai,
		vi,
	}
	return mi, nil
}

func (mon *Monitor) Fetch(start, end time.Time, step time.Duration) (*FetchResultDB, error) {
	var err, err2 error

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}

	fr := &FetchResultDB{
		Filename: config.Database.Type + ":" + config.Database.Dsn,  // XXX: old, not used
		Cf:       "AVERAGE",  // XXX: not AVERAGE, just ABSOLUTE now, not used
		Start:    start,
		End:      end,
		Step:     time.Duration(mon.Step) * time.Second,
		DsNames:  make([]string, len(mon.Values)),
		RowCnt:   0,
		DsData:   make([]*FetchResultDBItem, 0),
	}
	//fr.DsNames = make([]string, len(mon.Values))
	//fr.DsData = make([]*FetchResultDBItem, 0)

	var sensor_val_id int;
	var detection float64;
	var tm, sensor_id, derror string;

	// Load detections
	rows, err := tx.Stmt(stmts["detections_select_by_monitor_time_range"]).Query(
		mon.Id,
		start.Format(time.RFC3339Nano),
		end.Format(time.RFC3339Nano),
	)
	if err != nil {
		//fmt.Printf(LPURPLE+"Fetch:"+RED+" Fatal Detections Select Time Range Stmt Query %s, exiting\n"+NCO, err)
		logger.Print("Fatal Detections Select Time Range Stmt Query: " + err.Error())
		err2 = tx.Rollback()
		if err2 != nil {
			//fmt.Printf(LPURPLE+"Fetch:"+RED+" Fatal Detections Select Time Range Rollback %s, exiting\n"+NCO, err2)
			logger.Print("Fatal Detections Select Time Range Stmt Rollback: " + err2.Error())
			return nil, err2
		}
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		err = rows.Scan(&tm, &sensor_id, &sensor_val_id, &detection, &derror)
		if err != nil {
			//fmt.Printf(LPURPLE+"Fetch:"+RED+" Fatal Detections Select Time Range Scan, exiting\n"+NCO, err)
			logger.Printf("Fatal Detections Select Time Range Scan: %s", err.Error())
			err2 = tx.Rollback()
			if err2 != nil {
				//fmt.Printf(LPURPLE+"Fetch:"+RED+" Fatal Detections Select Time Range Scan Rollback %s, exiting\n"+NCO, err2)
				logger.Printf("Fatal Detections Select Time Range Scan Rollback: %s", err2.Error())
				return nil, err2
			}
			return nil, err
		}

		t, _ := time.Parse(time.RFC3339Nano, tm)
		
		// Link with DsNames by name
		// Search Name by unique sensor info
		name := ""
		for _, v := range mon.Values {
			if v.Sensor == sensor_id && v.ValueIdx == sensor_val_id {
				name = v.Name
				break
			}
		}

		fr.DsData = append(fr.DsData, &FetchResultDBItem{t, name, detection, derror});
	}

	err = tx.Commit()
	if err != nil {
		//fmt.Printf(LPURPLE+"Fetch:"+RED+" Fatal Commit Detections Select Time Range %s, exiting\n"+NCO, err)
		logger.Printf("Fatal Commit Detections Select Time Range %s, exiting\n", err.Error())
		return nil, err
	}

	fr.RowCnt = len(fr.DsData)

	for i := range fr.DsNames {
		fr.DsNames[i] = mon.Values[i].Name;
	}

	return fr, err
}

func (mon *Monitor) Remove(wdata bool) error {
	var err,err2 error
	//var res sql.Result

	if mon.Active {
		err = mon.Stop()
		if err != nil {
			logger.Print("error stopping monitor being removed: " + err.Error())
		}
	}
	delete(monitors, string(mon.UUID))
	
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	// Delete monitor detections data
	if wdata {
		_, err = tx.Stmt(stmts["detections_delete_by_monitor"]).Exec(mon.Id)
		if err != nil {
			logger.Print("error removing monitor data: " + err.Error())
		}
	}

	// Delete monitor values
	mon.Values = nil
	_, err = tx.Stmt(stmts["monitors_values_delete_by_uuid"]).Exec(mon.UUID.String())
	if err != nil {
		logger.Print("error removing monitor values: " + err.Error())
	}

	// Delete monitor
	_, err = tx.Stmt(stmts["monitors_delete_by_id"]).Exec(mon.Id)
	if err != nil {
		logger.Print("error removing monitor configuration: " + err.Error())
	}

	if err != nil {
		err2 = tx.Rollback()
		if err2 != nil {
			//fmt.Printf(LPURPLE+"Remove:"+RED+" Fatal Rollback Monitor Remove %s, exiting\n"+NCO, err2)
			logger.Printf("Fatal Rollback Monitor Remove: %s", err2.Error())
		}
	} else {
		err = tx.Commit()
		if err != nil {
			//fmt.Printf(LPURPLE+"Remove:"+RED+" Fatal Commit Monitor Remove %s, exiting\n"+NCO, err)
			logger.Printf("Fatal Commit Monitor Remove %s, exiting\n", err.Error())
		}
	}

	// Result error
	if err != nil {
		err = errors.New("error removing monitor: " + err.Error())
	}
	return err
}

func newMonitor(opts *MonitorOpts) (*Monitor, error) {
	if (!opts.StopAt.IsZero()) && opts.StopAt.Before(time.Now()) {
		err := errors.New("monitor stop time is in the past")
		return nil, err
	}
	vals := make([]MonValue, len(opts.Values))
	for i, v := range opts.Values {
		ok, errcode := valueAvailable(v.Sensor, v.ValueIdx)
		if !ok {
			switch errcode {
			case 1:
				err := errors.New("no sensor '" + v.Sensor + "' connected")
				return nil, err

			case 2:
				err := fmt.Errorf("no value %d for sensor '%s' available", v.ValueIdx, v.Sensor)
				return nil, err

			default:
				err := errors.New("Wrong sensor spec")
				return nil, err
			}
		}

		vals[i] = MonValue{
			pluggedSensors[v.Sensor].Values[v.ValueIdx].Name + strconv.Itoa(i),
			v.Sensor,
			v.ValueIdx,
			pluggedSensors[v.Sensor].Values[v.ValueIdx].Type,
			0,
		}
	}

	mon := Monitor{
		0,
		uuid.NewRandom(),
		opts.Exp_id,
		opts.Setup_id,
		opts.Step,
		opts.Count,
		time.Now(),
		opts.StopAt,
		true,
		nil,
		vals,
	}

	return &mon, nil
}

func createRunMonitor(opts *MonitorOpts) (*Monitor, error) {
	mon, err := newMonitor(opts)
	if err != nil {
		return mon, err
	}
	err = mon.Save()
	if err != nil {
		return mon, err
	}
	err = mon.Run()
	if err != nil {
		return mon, err
	}
	monitors[string(mon.UUID)] = mon

	return mon, nil
}
