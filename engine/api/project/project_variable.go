package project

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/go-gorp/gorp"

	"github.com/ovh/cds/engine/api/keys"
	"github.com/ovh/cds/engine/api/secret"
	"github.com/ovh/cds/sdk"
)

// GetVariableAudit Get variable audit for the given project
func GetVariableAudit(db gorp.SqlExecutor, key string) ([]sdk.VariableAudit, error) {
	audits := []sdk.VariableAudit{}
	query := `
		SELECT project_variable_audit_old.id, project_variable_audit_old.versionned, project_variable_audit_old.data, project_variable_audit_old.author
		FROM project_variable_audit_old
		JOIN project ON project.id = project_variable_audit_old.project_id
		WHERE project.projectkey = $1
		ORDER BY project_variable_audit_old.versionned DESC
	`
	rows, err := db.Query(query, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var audit sdk.VariableAudit
		var data string
		err := rows.Scan(&audit.ID, &audit.Versionned, &data, &audit.Author)
		if err != nil {
			return nil, err
		}
		var vars []sdk.Variable
		err = json.Unmarshal([]byte(data), &vars)
		if err != nil {
			return nil, err
		}
		audit.Variables = vars
		for i := range audit.Variables {
			v := &audit.Variables[i]
			if sdk.NeedPlaceholder(v.Type) {
				v.Value = sdk.PasswordPlaceholder
			}
		}

		audits = append(audits, audit)
	}
	return audits, nil
}

// GetAudit retrieve the current project variable audit
func GetAudit(db gorp.SqlExecutor, key string, auditID int64) ([]sdk.Variable, error) {
	query := `
		SELECT project_variable_audit_old.data
		FROM project_variable_audit_old
		JOIN project ON project.id = project_variable_audit_old.project_id
		WHERE project.projectkey = $1 AND project_variable_audit_old.id = $2
		ORDER BY project_variable_audit_old.versionned DESC
	`
	var data string
	err := db.QueryRow(query, key, auditID).Scan(&data)
	if err != nil {
		return nil, err
	}
	var variables []sdk.Variable
	err = json.Unmarshal([]byte(data), &variables)
	for i := range variables {
		v := &variables[i]
		if sdk.NeedPlaceholder(v.Type) {
			decode, err := base64.StdEncoding.DecodeString(v.Value)
			if err != nil {
				return nil, err
			}
			v.Value = string(decode)
		}
	}

	return variables, err
}

// CreateAudit Create variable audit for the given project
func CreateAudit(db gorp.SqlExecutor, proj *sdk.Project, u *sdk.User) error {
	variables, err := GetAllVariableInProject(db, proj.ID, WithEncryptPassword())
	if err != nil {
		return err
	}
	for i := range variables {
		v := &variables[i]
		if sdk.NeedPlaceholder(v.Type) {
			v.Value = base64.StdEncoding.EncodeToString([]byte(v.Value))
		}
	}

	data, err := json.Marshal(variables)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO project_variable_audit_old (versionned, project_id, data, author)
		VALUES (NOW(), $1, $2, $3)
	`
	_, err = db.Exec(query, proj.ID, string(data), u.Username)
	return err
}

// CheckVariableInProject check if the variable is already in the project or not
func CheckVariableInProject(db gorp.SqlExecutor, projectID int64, varName string) (bool, error) {
	query := `SELECT COUNT(id) FROM project_variable WHERE project_id = $1 AND var_name = $2`

	var nb int64
	err := db.QueryRow(query, projectID, varName).Scan(&nb)
	if err != nil {
		return false, err
	}
	if nb != 0 {
		return true, nil
	}
	return false, nil
}

type structarg struct {
	clearsecret   bool
	encryptsecret bool
}

// GetAllVariableFuncArg defines the base type for functional argument of GetAllVariable
type GetAllVariableFuncArg func(args *structarg)

// WithClearPassword is a function argument to GetAllVariableInProject
func WithClearPassword() GetAllVariableFuncArg {
	return func(args *structarg) {
		args.clearsecret = true
	}
}

// WithEncryptPassword is a function argument to GetAllVariableInProject.
func WithEncryptPassword() GetAllVariableFuncArg {
	return func(args *structarg) {
		args.encryptsecret = true
	}
}

// GetAllVariableInProject Get all variable for the given project
func GetAllVariableInProject(db gorp.SqlExecutor, projectID int64, args ...GetAllVariableFuncArg) ([]sdk.Variable, error) {
	c := structarg{}
	for _, f := range args {
		f(&c)
	}

	variables := []sdk.Variable{}
	query := `SELECT id, var_name, var_value, cipher_value, var_type
	          FROM project_variable
	          WHERE project_id=$1
	          ORDER BY var_name`
	rows, err := db.Query(query, projectID)
	if err != nil {
		return variables, err
	}
	defer rows.Close()
	for rows.Next() {
		var v sdk.Variable
		var typeVar string
		var clearVal sql.NullString
		var cipherVal []byte
		err = rows.Scan(&v.ID, &v.Name, &clearVal, &cipherVal, &typeVar)
		if err != nil {
			return nil, err
		}
		v.Type = typeVar
		if c.encryptsecret && sdk.NeedPlaceholder(v.Type) {
			v.Value = string(cipherVal)
		} else {
			v.Value, err = secret.DecryptS(v.Type, clearVal, cipherVal, c.clearsecret)
		}
		if err != nil {
			return nil, err
		}
		variables = append(variables, v)
	}
	return variables, err
}

// GetAllVariableNameInProjectByKey Get all variable for the given project
func GetAllVariableNameInProjectByKey(db gorp.SqlExecutor, projectKey string) ([]string, error) {
	variables := []string{}
	query := `SELECT project_variable.var_name
	          FROM project_variable
	          JOIN project ON project.id = project_variable.project_id
	          WHERE project.projectKey = $1
	          ORDER BY var_name`
	rows, err := db.Query(query, projectKey)
	if err != nil {
		return variables, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		err = rows.Scan(&name)
		if err != nil {
			return nil, err
		}
		variables = append(variables, name)
	}
	return variables, err
}

// GetVariableByID Get a project variable by its ID
func GetVariableByID(db gorp.SqlExecutor, projectID int64, variableID int64, args ...GetAllVariableFuncArg) (*sdk.Variable, error) {
	c := structarg{}
	for _, f := range args {
		f(&c)
	}

	variable := &sdk.Variable{}
	query := `SELECT id, var_name, var_value, var_type, cipher_value FROM project_variable
		  WHERE id=$1 AND project_id=$2`
	var varValue sql.NullString
	var cipher_value []byte
	err := db.QueryRow(query, variableID, projectID).Scan(&variable.ID, &variable.Name, &varValue, &variable.Type, &cipher_value)
	if err != nil {
		return variable, err
	}

	var errD error
	variable.Value, errD = secret.DecryptS(variable.Type, varValue, cipher_value, c.clearsecret)
	return variable, errD
}

// GetVariableInProject get the variable information for the given project
func GetVariableInProject(db gorp.SqlExecutor, projectID int64, variableName string, args ...GetAllVariableFuncArg) (*sdk.Variable, error) {
	c := structarg{}
	for _, f := range args {
		f(&c)
	}

	variable := &sdk.Variable{}
	query := `SELECT id, var_name, var_value, var_type, cipher_value FROM project_variable
		  WHERE var_name=$1 AND project_id=$2`
	var varValue sql.NullString
	var cipher_value []byte
	err := db.QueryRow(query, variableName, projectID).Scan(&variable.ID, &variable.Name, &varValue, &variable.Type, &cipher_value)
	if err != nil {
		return variable, err
	}
	var errD error
	variable.Value, errD = secret.DecryptS(variable.Type, varValue, cipher_value, c.clearsecret)
	return variable, errD
}

// InsertVariable Insert a new variable in the given project
func InsertVariable(db gorp.SqlExecutor, proj *sdk.Project, variable *sdk.Variable, u *sdk.User) error {
	query := `INSERT INTO project_variable(project_id, var_name, var_value, cipher_value, var_type)
		  VALUES($1, $2, $3, $4, $5) RETURNING id`

	clear, cipher, err := secret.EncryptS(variable.Type, variable.Value)
	if err != nil {
		return sdk.WrapError(err, "InsertVariable> Cannot encryp secret %s", variable.Name)
	}

	if err := db.QueryRow(query, proj.ID, variable.Name, clear, cipher, string(variable.Type)).Scan(&variable.ID); err != nil {
		return sdk.WrapError(err, "InsertVariable> Cannot insert variable %s in DB", variable.Name)
	}

	pva := &sdk.ProjectVariableAudit{
		ProjectID:     proj.ID,
		Type:          sdk.AuditAdd,
		VariableID:    variable.ID,
		VariableAfter: variable,
		Author:        u.Username,
		Versionned:    time.Now(),
	}

	if err := InsertAudit(db, pva); err != nil {
		return sdk.WrapError(err, "InsertVariable> Cannot insert audit for variable %s", variable.Name)
	}
	return nil
}

// UpdateVariable Update a variable in the given project
func UpdateVariable(db gorp.SqlExecutor, proj *sdk.Project, variable *sdk.Variable, u *sdk.User) error {
	// Clear password for audit
	previousVar, err := GetVariableByID(db, proj.ID, variable.ID, WithClearPassword())

	// If we are updating a batch of variables, some of them might be secrets, we don't want to crush the value
	if sdk.NeedPlaceholder(variable.Type) && variable.Value == sdk.PasswordPlaceholder {
		return nil
	}

	clear, cipher, err := secret.EncryptS(variable.Type, variable.Value)
	if err != nil {
		return sdk.WrapError(err, "UpdateVariable> Cannot encrypt secret %s", variable.Name)
	}

	query := `UPDATE project_variable SET var_name=$1, var_value=$2, cipher_value=$3, var_type=$4
		   WHERE id=$5`
	_, err = db.Exec(query, variable.Name, clear, cipher, string(variable.Type), variable.ID)
	if err != nil {
		return sdk.WrapError(err, "UpdateVariable> Cannot update variable %s", variable.Name)
	}

	pva := &sdk.ProjectVariableAudit{
		ProjectID:      proj.ID,
		Type:           sdk.AuditUpdate,
		VariableID:     variable.ID,
		VariableBefore: previousVar,
		VariableAfter:  variable,
		Author:         u.Username,
		Versionned:     time.Now(),
	}

	if err := InsertAudit(db, pva); err != nil {
		return sdk.WrapError(err, "UpdateVariable> Cannot insert audit for variable %s", variable.Name)
	}

	return nil
}

// DeleteVariable Delete a variable from the given project
func DeleteVariable(db gorp.SqlExecutor, proj *sdk.Project, variable *sdk.Variable, u *sdk.User) error {
	query := `DELETE FROM project_variable WHERE project_id=$1 AND id=$2`
	_, err := db.Exec(query, proj.ID, variable.ID)
	if err != nil {
		return sdk.WrapError(err, "DeleteVariable> Cannot delete variable %s", variable.Name)
	}

	pva := &sdk.ProjectVariableAudit{
		ProjectID:      proj.ID,
		Type:           sdk.AuditDelete,
		VariableID:     variable.ID,
		VariableBefore: variable,
		Author:         u.Username,
		Versionned:     time.Now(),
	}

	if err := InsertAudit(db, pva); err != nil {
		return sdk.WrapError(err, "DeleteVariable> Cannot insert audit for variable %s", variable.Name)
	}

	return err
}

// DeleteAllVariable Delete all variables from the given project
func DeleteAllVariable(db gorp.SqlExecutor, projectID int64) error {
	query := `DELETE FROM project_variable WHERE project_id=$1`
	_, err := db.Exec(query, projectID)
	if err != nil {
		return err
	}

	return err
}

// AddKeyPair generate a ssh key pair and add them as project variables
func AddKeyPair(db gorp.SqlExecutor, proj *sdk.Project, keyname string, u *sdk.User) error {
	pub, priv, errGenerate := keys.Generatekeypair(keyname)
	if errGenerate != nil {
		return errGenerate
	}

	v := &sdk.Variable{
		Name:  keyname,
		Type:  sdk.KeyVariable,
		Value: priv,
	}

	if err := InsertVariable(db, proj, v, u); err != nil {
		return err
	}

	p := &sdk.Variable{
		Name:  keyname + ".pub",
		Type:  sdk.TextVariable,
		Value: pub,
	}

	return InsertVariable(db, proj, p, u)
}

// InsertAudit insert an audit on a project variable
func InsertAudit(db gorp.SqlExecutor, pva *sdk.ProjectVariableAudit) error {
	dbProjVarAudit := dbProjectVariableAudit(*pva)
	if err := db.Insert(&dbProjVarAudit); err != nil {
		return sdk.WrapError(err, "Cannot insert audit for variable %d", pva.VariableID)
	}
	*pva = sdk.ProjectVariableAudit(dbProjVarAudit)
	return nil
}

// LoadVariableAudits Load audits for the given variable
func LoadVariableAudits(db gorp.SqlExecutor, projectID, varID int64) ([]sdk.ProjectVariableAudit, error) {
	var res []dbProjectVariableAudit
	query := "SELECT * FROM project_variable_audit WHERE project_id = $1 AND variable_id = $2 ORDER BY versionned DESC"
	if _, err := db.Select(&res, query, projectID, varID); err != nil {
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if err != nil && err == sql.ErrNoRows {
			return []sdk.ProjectVariableAudit{}, nil
		}
	}

	pvas := make([]sdk.ProjectVariableAudit, len(res))
	for i := range res {
		dbPva := &res[i]
		if err := dbPva.PostGet(db); err != nil {
			return nil, err
		}
		pva := sdk.ProjectVariableAudit(*dbPva)
		pvas[i] = pva
	}
	return pvas, nil
}
