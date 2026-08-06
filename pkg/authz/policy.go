package authz

import (
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	annotationsv1 "github.com/eagle-go/eagle/api/eagle/annotations/v1"
)

// Policy 是某个 RPC 方法的访问策略，来自 proto 上的
// eagle.annotations.v1.perm / public 扩展。
type Policy struct {
	// Perm 是调用所需的权限码，空表示只校验登录态。
	Perm string
	// Public 表示免登录。
	Public bool
	// Known 表示成功解析到了方法描述符。
	//
	// 解析不到时（比如 gRPC 反射、健康检查这类非本项目定义的方法）
	// 按"需要登录但不需要具体权限"处理——失败方向选择拒绝而非放行。
	Known bool
}

// 描述符查找要走全局注册表并做接口断言，开销不小，
// 而 operation 集合是有限且固定的，所以查一次就缓存住。
var policyCache sync.Map // operation(string) -> Policy

// PolicyFor 解析 Kratos 的 operation（形如 "/eagle.system.v1.UserService/CreateUser"）
// 并返回该方法的访问策略。
func PolicyFor(operation string) Policy {
	if v, ok := policyCache.Load(operation); ok {
		p, _ := v.(Policy)
		return p
	}
	p := lookupPolicy(operation)
	policyCache.Store(operation, p)
	return p
}

func lookupPolicy(operation string) Policy {
	serviceName, methodName, ok := splitOperation(operation)
	if !ok {
		return Policy{}
	}

	desc, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(serviceName))
	if err != nil {
		return Policy{}
	}
	sd, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		return Policy{}
	}
	md := sd.Methods().ByName(protoreflect.Name(methodName))
	if md == nil {
		return Policy{}
	}
	opts, ok := md.Options().(*descriptorpb.MethodOptions)
	if !ok || opts == nil {
		return Policy{Known: true}
	}

	p := Policy{Known: true}
	if v, ok := proto.GetExtension(opts, annotationsv1.E_Perm).(string); ok {
		p.Perm = v
	}
	if v, ok := proto.GetExtension(opts, annotationsv1.E_Public).(bool); ok {
		p.Public = v
	}
	return p
}

// splitOperation 把 "/pkg.Service/Method" 拆成服务全名和方法名。
func splitOperation(operation string) (service, method string, ok bool) {
	s := strings.TrimPrefix(operation, "/")
	idx := strings.LastIndex(s, "/")
	if idx <= 0 || idx == len(s)-1 {
		return "", "", false
	}
	return s[:idx], s[idx+1:], true
}
