package service

import (
	"fmt"
	"strings"

	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

// ---------- 角色管理(role_service.py) ----------

// ListRoles 角色列表(含权限/资源ID集)。
func ListRoles(page, size int, keyword string) (*Page, error) {
	where := ""
	args := []any{}
	if keyword != "" {
		where = "WHERE name LIKE ? OR code LIKE ?"
		kw := "%" + keyword + "%"
		args = append(args, kw, kw)
	}
	total, err := qCount(`SELECT COUNT(*) FROM sys_role `+where, args...)
	if err != nil {
		return nil, err
	}
	rows, err := qList(`SELECT * FROM sys_role `+where+` ORDER BY id LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, err
	}
	list := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		perms, err := PermIDsOfRole(Int(r["id"]))
		if err != nil {
			return nil, err
		}
		res, err := ResIDsOfRole(Int(r["id"]))
		if err != nil {
			return nil, err
		}
		r["permissions"] = perms
		r["resources"] = res
		list = append(list, r)
	}
	return &Page{List: list, Total: total, PageV: page, Size: size}, nil
}

// PermIDsOfRole 角色权限ID集。
func PermIDsOfRole(roleID int64) ([]any, error) {
	rows, err := qList(`SELECT permission_id FROM sys_role_permission WHERE role_id = ?`, roleID)
	if err != nil {
		return nil, err
	}
	return idList(rows, "permission_id"), nil
}

// ResIDsOfRole 角色资源ID集。
func ResIDsOfRole(roleID int64) ([]any, error) {
	rows, err := qList(`SELECT resource_id FROM sys_role_resource WHERE role_id = ?`, roleID)
	if err != nil {
		return nil, err
	}
	return idList(rows, "resource_id"), nil
}

func idList(rows []map[string]any, key string) []any {
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, Int(r[key]))
	}
	return out
}

// ---------- 授予面收敛:保留字黑名单 + 授权上界(containment) ----------

// reservedRoleCodes 系统保留角色码(须与 auth.go GetPermissionCodes 的通配超管位保持同步):
// 这些码在权限判定中等价于通配 "*",不得经角色 CRUD 创建,也不得作为父级被继承。
var reservedRoleCodes = map[string]bool{"admin": true, "ROLE-ADMIN": true}

// reservedPermissionCodes 保留权限码:"*" 只能由代码层的内置超管位注入,不得经权限 CRUD 落库。
var reservedPermissionCodes = map[string]bool{"*": true}

// IsReservedRoleCode 保留角色码判定(去空白、大小写不敏感;比 auth.go 的精确比较更宽)。
func IsReservedRoleCode(code string) bool {
	code = strings.TrimSpace(code)
	for rc := range reservedRoleCodes {
		if strings.EqualFold(code, rc) {
			return true
		}
	}
	return false
}

// IsReservedPermissionCode 保留权限码判定。
func IsReservedPermissionCode(code string) bool {
	return reservedPermissionCodes[strings.TrimSpace(code)]
}

// RoleGrantScope 取 actor 的可授出范围(其有效权限码集;含 "*" 表示超管,不受上界约束)。
func RoleGrantScope(actorID int64) (map[string]bool, error) {
	codes, err := GetPermissionCodes(nil, actorID)
	if err != nil {
		return nil, err
	}
	scope := make(map[string]bool, len(codes))
	for _, c := range codes {
		scope[c] = true
	}
	return scope, nil
}

// AuthorizablePermIDs 校验权限ID集是否超出 scope(超管豁免);返回越界说明,空=通过。
func AuthorizablePermIDs(scope map[string]bool, permIDs []any) ([]string, error) {
	if scope["*"] {
		return nil, nil
	}
	var out []string
	for _, pid := range permIDs {
		row, err := qOne(`SELECT code FROM sys_permission WHERE id = ?`, Int(pid))
		if err != nil {
			return nil, err
		}
		if row == nil {
			out = append(out, fmt.Sprintf("权限#%d 不存在", Int(pid)))
			continue
		}
		if code := Str(row["code"]); !scope[code] {
			out = append(out, "权限:"+code)
		}
	}
	return out, nil
}

// AuthorizableRoleIDs 校验角色ID集(含父级继承的权限)是否超出 scope(超管豁免);
// 返回越界说明,空=通过。保留超管码角色视为越界(仅超管可授)。
func AuthorizableRoleIDs(scope map[string]bool, roleIDs []any) ([]string, error) {
	if scope["*"] {
		return nil, nil
	}
	var out []string
	for _, rid := range roleIDs {
		role, err := qOne(`SELECT id, code FROM sys_role WHERE id = ?`, Int(rid))
		if err != nil {
			return nil, err
		}
		if role == nil {
			out = append(out, fmt.Sprintf("角色#%d 不存在", Int(rid)))
			continue
		}
		codes, err := roleEffectiveCodes(Int(role["id"]))
		if err != nil {
			return nil, err
		}
		for _, c := range codes {
			if c == "*" || !scope[c] {
				out = append(out, "角色:"+Str(role["code"]))
				break
			}
		}
	}
	return out, nil
}

// roleEffectiveCodes 角色(沿父级向上)生效的权限码;链路含保留超管码时返回 "*"。
func roleEffectiveCodes(roleID int64) ([]string, error) {
	seen := map[int64]bool{}
	stack := []int64{roleID}
	codes := map[string]bool{}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		role, err := qOne(`SELECT id, code, parent_id, status FROM sys_role WHERE id = ?`, id)
		if err != nil {
			return nil, err
		}
		if role == nil || Int(role["status"]) != 1 {
			continue
		}
		if IsReservedRoleCode(Str(role["code"])) {
			return []string{"*"}, nil
		}
		perms, err := qList(`SELECT p.code FROM sys_permission p
			JOIN sys_role_permission rp ON rp.permission_id = p.id
			WHERE rp.role_id = ? AND p.status = 1`, id)
		if err != nil {
			return nil, err
		}
		for _, p := range perms {
			codes[Str(p["code"])] = true
		}
		if pid := Int(role["parent_id"]); pid != 0 && !seen[pid] {
			stack = append(stack, pid)
		}
	}
	out := make([]string, 0, len(codes))
	for c := range codes {
		out = append(out, c)
	}
	return out, nil
}

// validateRoleParent 角色父级安全校验:0=无父级;拒绝自指、保留超管码父级与不存在的父级
// (未受约束的 parent_id 可让普通角色继承超管角色的通配权限)。
func validateRoleParent(roleID, parentID int64) error {
	if parentID == 0 {
		return nil
	}
	if roleID != 0 && parentID == roleID {
		return Fail(errcode.ErrParam, "父级角色不能是自身")
	}
	parent, err := qOne(`SELECT id, code FROM sys_role WHERE id = ?`, parentID)
	if err != nil {
		return err
	}
	if parent == nil {
		return Fail(errcode.ErrParam, "父级角色不存在")
	}
	if IsReservedRoleCode(Str(parent["code"])) {
		return Fail(errcode.ErrParam, "父级角色为系统保留超管角色，不允许继承")
	}
	return nil
}

// GetRole 角色详情。
func GetRole(roleID int64) (map[string]any, error) {
	r, err := qOne(`SELECT * FROM sys_role WHERE id = ?`, roleID)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, NotFound("角色")
	}
	perms, err := PermIDsOfRole(roleID)
	if err != nil {
		return nil, err
	}
	res, err := ResIDsOfRole(roleID)
	if err != nil {
		return nil, err
	}
	r["permissions"] = perms
	r["resources"] = res
	return r, nil
}

// CreateRole 新建角色。
func CreateRole(data map[string]any) (int64, error) {
	code := strings.TrimSpace(Str(data["code"]))
	if code == "" {
		return 0, Fail(errcode.ErrParam, "角色编码必填")
	}
	if IsReservedRoleCode(code) {
		return 0, Fail(errcode.ErrParam, "角色编码为系统保留")
	}
	parentID := Int(orDefaultNum(data["parent_id"], 0))
	if err := validateRoleParent(0, parentID); err != nil {
		return 0, err
	}
	if exists, err := qOne(`SELECT id FROM sys_role WHERE code = ?`, code); err != nil {
		return 0, err
	} else if exists != nil {
		return 0, DupErr("角色编码")
	}
	if _, err := qExec(`
		INSERT INTO sys_role (name, code, parent_id, description, status) VALUES (?, ?, ?, ?, ?)`,
		data["name"], code, parentID, data["description"], orDefaultNum(data["status"], 1)); err != nil {
		return 0, err
	}
	created, err := qOne(`SELECT id FROM sys_role WHERE code = ?`, code)
	if err != nil {
		return 0, err
	}
	roleID := Int(created["id"])
	if permIDs, ok := data["permission_ids"].([]any); ok && len(permIDs) > 0 {
		if err := SetRolePermissions(roleID, permIDs); err != nil {
			return 0, err
		}
	}
	if resIDs, ok := data["resource_ids"].([]any); ok && len(resIDs) > 0 {
		if err := SetRoleResources(roleID, resIDs); err != nil {
			return 0, err
		}
	}
	return roleID, nil
}

// UpdateRole 编辑角色(编码不可改)。
func UpdateRole(roleID int64, data map[string]any) error {
	existing, err := qOne(`SELECT * FROM sys_role WHERE id = ?`, roleID)
	if err != nil {
		return err
	}
	if existing == nil {
		return NotFound("角色")
	}
	name := data["name"]
	if name == nil {
		name = existing["name"]
	}
	desc := data["description"]
	if desc == nil {
		desc = existing["description"]
	}
	parentID := Int(orDefaultNum(data["parent_id"], 0))
	if err := validateRoleParent(roleID, parentID); err != nil {
		return err
	}
	_, err = qExec(`
		UPDATE sys_role SET name=?, parent_id=?, description=?, status=?, updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
		WHERE id=?`,
		name, parentID, desc, orDefaultNum(data["status"], 1), roleID)
	return err
}

// DeleteRole 删除角色(级联清理关联)。
func DeleteRole(roleID int64) error {
	if exists, err := qOne(`SELECT id FROM sys_role WHERE id = ?`, roleID); err != nil {
		return err
	} else if exists == nil {
		return NotFound("角色")
	}
	for _, sql := range []string{
		`DELETE FROM sys_user_role WHERE role_id = ?`,
		`DELETE FROM sys_role_permission WHERE role_id = ?`,
		`DELETE FROM sys_role_resource WHERE role_id = ?`,
		`DELETE FROM sys_role WHERE id = ?`,
	} {
		if _, err := qExec(sql, roleID); err != nil {
			return err
		}
	}
	return nil
}

// AssignPermissions 分配权限(全量替换)。
func AssignPermissions(roleID int64, permIDs []any) error {
	return SetRolePermissions(roleID, permIDs)
}

// AssignResources 分配资源(全量替换)。
func AssignResources(roleID int64, resIDs []any) error {
	return SetRoleResources(roleID, resIDs)
}

// SetRolePermissions 全量替换角色权限。
func SetRolePermissions(roleID int64, permIDs []any) error {
	if _, err := qExec(`DELETE FROM sys_role_permission WHERE role_id = ?`, roleID); err != nil {
		return err
	}
	for _, pid := range permIDs {
		if _, err := qExec(`INSERT INTO sys_role_permission (role_id, permission_id) VALUES (?, ?)`, roleID, pid); err != nil {
			return err
		}
	}
	return nil
}

// SetRoleResources 全量替换角色资源。
func SetRoleResources(roleID int64, resIDs []any) error {
	if _, err := qExec(`DELETE FROM sys_role_resource WHERE role_id = ?`, roleID); err != nil {
		return err
	}
	for _, rid := range resIDs {
		if _, err := qExec(`INSERT INTO sys_role_resource (role_id, resource_id) VALUES (?, ?)`, roleID, rid); err != nil {
			return err
		}
	}
	return nil
}

// ---------- 权限管理(permission_service.py) ----------

// ListPermissions 权限列表。
func ListPermissions(page, size int, keyword string) (*Page, error) {
	where := ""
	args := []any{}
	if keyword != "" {
		where = "WHERE code LIKE ? OR name LIKE ?"
		kw := "%" + keyword + "%"
		args = append(args, kw, kw)
	}
	total, err := qCount(`SELECT COUNT(*) FROM sys_permission `+where, args...)
	if err != nil {
		return nil, err
	}
	rows, err := qList(`SELECT * FROM sys_permission `+where+` ORDER BY id LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	return &Page{List: rows, Total: total, PageV: page, Size: size}, nil
}

// ListAllPermissions 全量权限(下拉)。
func ListAllPermissions() ([]map[string]any, error) {
	rows, err := qList(`SELECT id, code, name FROM sys_permission WHERE status = 1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	return rows, nil
}

// CreatePermission 新建权限。
func CreatePermission(data map[string]any) (int64, error) {
	code := strings.TrimSpace(Str(data["code"]))
	if code == "" {
		return 0, Fail(errcode.ErrParam, "权限编码必填")
	}
	if IsReservedPermissionCode(code) {
		return 0, Fail(errcode.ErrParam, "权限编码为系统保留")
	}
	if exists, err := qOne(`SELECT id FROM sys_permission WHERE code = ?`, code); err != nil {
		return 0, err
	} else if exists != nil {
		return 0, DupErr("权限编码")
	}
	if _, err := qExec(`
		INSERT INTO sys_permission (code, name, target_type, target_ref, data_scope, abac_condition, status)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		code, data["name"], data["target_type"], data["target_ref"],
		orDefault(data["data_scope"], "ALL"), data["abac_condition"], orDefaultNum(data["status"], 1)); err != nil {
		return 0, err
	}
	created, err := qOne(`SELECT id FROM sys_permission WHERE code = ?`, code)
	if err != nil {
		return 0, err
	}
	return Int(created["id"]), nil
}

// UpdatePermission 编辑权限。
func UpdatePermission(permID int64, data map[string]any) error {
	existing, err := qOne(`SELECT * FROM sys_permission WHERE id = ?`, permID)
	if err != nil {
		return err
	}
	if existing == nil {
		return NotFound("权限")
	}
	pick := func(key string) any {
		if v, ok := data[key]; ok && v != nil {
			return v
		}
		return existing[key]
	}
	// 权限编码可改,同样拒绝保留码(通配 "*" 只能由代码层超管位注入)
	if code := Str(data["code"]); code != "" && IsReservedPermissionCode(code) {
		return Fail(errcode.ErrParam, "权限编码为系统保留")
	}
	_, err = qExec(`
		UPDATE sys_permission SET code=?, name=?, target_type=?, target_ref=?, data_scope=?, abac_condition=?, status=?
		WHERE id=?`,
		pick("code"), pick("name"), pick("target_type"), pick("target_ref"),
		pick("data_scope"), pick("abac_condition"), pick("status"), permID)
	return err
}

// DeletePermission 删除权限(级联清理关联)。
func DeletePermission(permID int64) error {
	if exists, err := qOne(`SELECT id FROM sys_permission WHERE id = ?`, permID); err != nil {
		return err
	} else if exists == nil {
		return NotFound("权限")
	}
	for _, sql := range []string{
		`DELETE FROM sys_role_permission WHERE permission_id = ?`,
		`DELETE FROM sys_permission WHERE id = ?`,
	} {
		if _, err := qExec(sql, permID); err != nil {
			return err
		}
	}
	return nil
}

// ---------- 资源管理(resource_service.py) ----------

// ListResources 全量资源。
func ListResources() ([]map[string]any, error) {
	rows, err := qList(`SELECT * FROM sys_resource ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	return rows, nil
}

// ResourceTree 资源树。
func ResourceTree() ([]map[string]any, error) {
	rows, err := ListResources()
	if err != nil {
		return nil, err
	}
	byID := map[int64]map[string]any{}
	for _, r := range rows {
		r["children"] = []map[string]any{}
		byID[Int(r["id"])] = r
	}
	var roots []map[string]any
	for _, r := range rows {
		parentID := Int(r["parent_id"])
		if parent, ok := byID[parentID]; ok && parentID != 0 {
			children := parent["children"].([]map[string]any)
			parent["children"] = append(children, r)
		} else {
			roots = append(roots, r)
		}
	}
	if roots == nil {
		roots = []map[string]any{}
	}
	return roots, nil
}

// CreateResource 新建资源。
func CreateResource(data map[string]any) (int64, error) {
	code := Str(data["code"])
	if code == "" {
		return 0, Fail(errcode.ErrParam, "资源编码必填")
	}
	if exists, err := qOne(`SELECT id FROM sys_resource WHERE code = ?`, code); err != nil {
		return 0, err
	} else if exists != nil {
		return 0, DupErr("资源编码")
	}
	rtype := Str(data["type"])
	if rtype == "MENU" && Str(data["path"]) == "" {
		return 0, Fail(errcode.ErrParam, "MENU 必须配置 path")
	}
	if (rtype == "BUTTON" || rtype == "API") && Str(data["permission_code"]) == "" {
		return 0, Fail(errcode.ErrParam, "BUTTON/API 必须配置 permission_code")
	}
	if _, err := qExec(`
		INSERT INTO sys_resource (parent_id, name, code, permission_code, type, path, component, icon, http_method, sort_order, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		orDefaultNum(data["parent_id"], 0), data["name"], code, data["permission_code"], rtype,
		data["path"], data["component"], data["icon"], data["http_method"],
		orDefaultNum(data["sort_order"], 0), orDefaultNum(data["status"], 1)); err != nil {
		return 0, err
	}
	created, err := qOne(`SELECT id FROM sys_resource WHERE code = ?`, code)
	if err != nil {
		return 0, err
	}
	return Int(created["id"]), nil
}

// UpdateResource 编辑资源。
func UpdateResource(resID int64, data map[string]any) error {
	existing, err := qOne(`SELECT * FROM sys_resource WHERE id = ?`, resID)
	if err != nil {
		return err
	}
	if existing == nil {
		return NotFound("资源")
	}
	pick := func(key string) any {
		if v, ok := data[key]; ok && v != nil {
			return v
		}
		return existing[key]
	}
	_, err = qExec(`
		UPDATE sys_resource SET parent_id=?, name=?, permission_code=?, type=?, path=?, component=?, icon=?, http_method=?, sort_order=?, status=?,
		updated_at=to_char(now(),'YYYY-MM-DD HH24:MI:SS')
		WHERE id=?`,
		pick("parent_id"), pick("name"), pick("permission_code"), pick("type"), pick("path"),
		pick("component"), pick("icon"), pick("http_method"), pick("sort_order"), pick("status"), resID)
	return err
}

// DeleteResource 删除资源(级联清理关联)。
func DeleteResource(resID int64) error {
	if exists, err := qOne(`SELECT id FROM sys_resource WHERE id = ?`, resID); err != nil {
		return err
	} else if exists == nil {
		return NotFound("资源")
	}
	for _, sql := range []string{
		`DELETE FROM sys_role_resource WHERE resource_id = ?`,
		`DELETE FROM sys_resource WHERE id = ?`,
	} {
		if _, err := qExec(sql, resID); err != nil {
			return err
		}
	}
	return nil
}
