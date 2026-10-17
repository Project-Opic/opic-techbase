// Package system —— 系统管理控制器(用户/角色/权限/资源;契约与旧版一致)。
package system

import (
	"strings"

	"github.com/gin-gonic/gin"

	"gitcode.com/opic-ontology/opic-techbase/internal/api/common"
	"gitcode.com/opic-ontology/opic-techbase/internal/service"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

// ---------- 用户管理 ----------

// UserList GET /api/users
func UserList(c *gin.Context) {
	page, size := common.PageOf(c)
	p, err := service.ListUsers(page, size, c.Query("keyword"))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, p, "")
}

// UserOptions GET /api/users/options(角色下拉)
func UserOptions(c *gin.Context) {
	rows, err := service.ListRoleOptions()
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, rows, "")
}

// UserCreate POST /api/users
func UserCreate(c *gin.Context) {
	body := common.BindJSON(c)
	// 新建用户可顺带授角色:同样受授予上界约束
	if roleIDs, ok := body["role_ids"].([]any); ok && len(roleIDs) > 0 {
		if !guardRoleGrant(c, roleIDs) {
			return
		}
	}
	id, err := service.CreateUser(body)
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, gin.H{"id": id}, "创建成功")
}

// UserUpdate PUT /api/users/:id
func UserUpdate(c *gin.Context) {
	if err := service.UpdateUser(common.PathID(c, "id"), common.BindJSON(c)); err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, nil, "更新成功")
}

// UserDelete DELETE /api/users/:id
func UserDelete(c *gin.Context) {
	if err := service.DeleteUser(common.PathID(c, "id")); err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, nil, "删除成功")
}

// UserAssignRoles PUT /api/users/:id/roles
func UserAssignRoles(c *gin.Context) {
	body := common.BindJSON(c)
	roleIDs, _ := body["role_ids"].([]any)
	if !guardRoleGrant(c, roleIDs) {
		return
	}
	if err := service.AssignRoles(common.PathID(c, "id"), roleIDs); err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, nil, "角色已更新")
}

// UserResetPwd PUT /api/users/:id/reset-pwd
func UserResetPwd(c *gin.Context) {
	body := common.BindJSON(c)
	if err := service.ResetPassword(common.PathID(c, "id"), service.Str(body["new_password"])); err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, nil, "密码已重置")
}

// ---------- 角色管理 ----------

// RoleList GET /api/roles
func RoleList(c *gin.Context) {
	page, size := common.PageOf(c)
	p, err := service.ListRoles(page, size, c.Query("keyword"))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, p, "")
}

// RoleCreate POST /api/roles
func RoleCreate(c *gin.Context) {
	body := common.BindJSON(c)
	// 建角色可顺带授权限:不得授出超出自身有效码集的权限
	if permIDs, ok := body["permission_ids"].([]any); ok && len(permIDs) > 0 {
		if !guardPermGrant(c, permIDs) {
			return
		}
	}
	// 父级继承同属授权通道:父级角色须在自身可授范围内
	if parentID := service.Int(body["parent_id"]); parentID != 0 {
		if !guardRoleGrant(c, []any{parentID}) {
			return
		}
	}
	id, err := service.CreateRole(body)
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, gin.H{"id": id}, "创建成功")
}

// RoleUpdate PUT /api/roles/:id
func RoleUpdate(c *gin.Context) {
	body := common.BindJSON(c)
	// 改父级即改变继承链(可继承父级全部权限):父级须在自身可授范围内
	if parentID := service.Int(body["parent_id"]); parentID != 0 {
		if !guardRoleGrant(c, []any{parentID}) {
			return
		}
	}
	if err := service.UpdateRole(common.PathID(c, "id"), body); err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, nil, "更新成功")
}

// RoleDelete DELETE /api/roles/:id
func RoleDelete(c *gin.Context) {
	if err := service.DeleteRole(common.PathID(c, "id")); err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, nil, "删除成功")
}

// RoleAssignPermissions PUT /api/roles/:id/permissions
func RoleAssignPermissions(c *gin.Context) {
	body := common.BindJSON(c)
	ids, _ := body["permission_ids"].([]any)
	if !guardPermGrant(c, ids) {
		return
	}
	if err := service.AssignPermissions(common.PathID(c, "id"), ids); err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, nil, "权限已更新")
}

// RoleAssignResources PUT /api/roles/:id/resources
func RoleAssignResources(c *gin.Context) {
	body := common.BindJSON(c)
	ids, _ := body["resource_ids"].([]any)
	if err := service.AssignResources(common.PathID(c, "id"), ids); err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, nil, "资源已更新")
}

// ---------- 权限管理 ----------

// PermissionList GET /api/permissions
func PermissionList(c *gin.Context) {
	page, size := common.PageOf(c)
	p, err := service.ListPermissions(page, size, c.Query("keyword"))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, p, "")
}

// PermissionAll GET /api/permissions/all
func PermissionAll(c *gin.Context) {
	rows, err := service.ListAllPermissions()
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, rows, "")
}

// PermissionCreate POST /api/permissions
func PermissionCreate(c *gin.Context) {
	id, err := service.CreatePermission(common.BindJSON(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, gin.H{"id": id}, "创建成功")
}

// PermissionUpdate PUT /api/permissions/:id
func PermissionUpdate(c *gin.Context) {
	body := common.BindJSON(c)
	// 改编码等同授出该码:新码须在自身有效码集内(保留码另由 service 层拒绝)
	if !guardPermCode(c, service.Str(body["code"])) {
		return
	}
	if err := service.UpdatePermission(common.PathID(c, "id"), body); err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, nil, "更新成功")
}

// guardPermCode 权限编码变更的授予上界校验(超管豁免);空值表示不改码。
func guardPermCode(c *gin.Context, code string) bool {
	code = strings.TrimSpace(code)
	if code == "" {
		return true
	}
	scope, err := service.RoleGrantScope(common.UserOf(c).ID)
	if err != nil {
		common.FailJSON(c, err)
		return false
	}
	if scope["*"] || scope[code] {
		return true
	}
	c.JSON(403, errcode.New(errcode.ErrForbidden, "不得将权限编码改为超出自身权限范围的值: "+code, nil))
	return false
}

// PermissionDelete DELETE /api/permissions/:id
func PermissionDelete(c *gin.Context) {
	if err := service.DeletePermission(common.PathID(c, "id")); err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, nil, "删除成功")
}

// ---------- 资源管理 ----------

// ResourceTree GET /api/resources/tree
func ResourceTree(c *gin.Context) {
	tree, err := service.ResourceTree()
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, tree, "")
}

// ResourceList GET /api/resources
func ResourceList(c *gin.Context) {
	rows, err := service.ListResources()
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, rows, "")
}

// ResourceCreate POST /api/resources
func ResourceCreate(c *gin.Context) {
	id, err := service.CreateResource(common.BindJSON(c))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, gin.H{"id": id}, "创建成功")
}

// ResourceUpdate PUT /api/resources/:id
func ResourceUpdate(c *gin.Context) {
	if err := service.UpdateResource(common.PathID(c, "id"), common.BindJSON(c)); err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, nil, "更新成功")
}

// ResourceDelete DELETE /api/resources/:id
func ResourceDelete(c *gin.Context) {
	if err := service.DeleteResource(common.PathID(c, "id")); err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, nil, "删除成功")
}

// ---------- 授予面 containment:不得授出超出自身有效码集的权限/角色 ----------
//
// 保留字黑名单(内置超管码/通配 "*")在服务层 CreateRole/CreatePermission 拒绝;
// 这里补授权上界:委派管理员只能授出自己已持有的权限码与角色(超管 "*" 豁免)。

// guardPermGrant 权限授予上界校验;越界直接写 403 并返回 false。
func guardPermGrant(c *gin.Context, permIDs []any) bool {
	msg, err := grantViolation(c, permIDs, true)
	return grantPass(c, msg, err)
}

// guardRoleGrant 角色授予上界校验(含父级继承与保留超管码);越界直接写 403 并返回 false。
func guardRoleGrant(c *gin.Context, roleIDs []any) bool {
	msg, err := grantViolation(c, roleIDs, false)
	return grantPass(c, msg, err)
}

// grantViolation 计算越界说明(空=通过);scope 取当前登录人的有效权限码集。
func grantViolation(c *gin.Context, ids []any, perms bool) (string, error) {
	scope, err := service.RoleGrantScope(common.UserOf(c).ID)
	if err != nil {
		return "", err
	}
	var out []string
	if perms {
		out, err = service.AuthorizablePermIDs(scope, ids)
	} else {
		out, err = service.AuthorizableRoleIDs(scope, ids)
	}
	if err != nil {
		return "", err
	}
	if len(out) == 0 {
		return "", nil
	}
	what := "角色"
	if perms {
		what = "权限"
	}
	return "不得授出超出自身权限范围的" + what + ": " + strings.Join(out, ", "), nil
}

// grantPass 按校验结果写响应:错误走统一信封,越界走 403(与 RequirePermission 口径一致)。
func grantPass(c *gin.Context, msg string, err error) bool {
	if err != nil {
		common.FailJSON(c, err)
		return false
	}
	if msg != "" {
		c.JSON(403, errcode.New(errcode.ErrForbidden, msg, nil))
		return false
	}
	return true
}
