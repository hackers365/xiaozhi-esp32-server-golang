# 配置文件说明

## 配置文件列表

- `config.json` - 默认配置文件
- `config.dev.json` - 开发环境配置
- `config.prod.json` - 生产环境配置
- `config.example.json` - 配置文件示例

## 配置文件结构

```json
{
  "server": {
    "port": "8080",        // 服务器端口
    "mode": "debug"        // 运行模式: debug/release
  },
  "database": {
    "host": "localhost",   // 数据库主机
    "port": "3306",        // 数据库端口
    "username": "root",    // 数据库用户名
    "password": "password", // 数据库密码
    "database": "xiaozhi_admin" // 数据库名称
  },
  "jwt": {
    "secret": "", // 启动前必须配置至少32字节的随机密钥，或设置JWT_SECRET环境变量
    "expire_hour": 24           // Token过期时间(小时)
  }
}
```

## 使用方法

启动后端前，请生成独立 JWT 密钥并设置 `JWT_SECRET`（例如 `openssl rand -hex 32`），或写入配置文件的 `jwt.secret`。环境变量优先。重启及多实例部署应保持同一密钥；轮换密钥会使已有 JWT 失效。

### 1. 命令行参数

```bash
# 使用默认配置文件
go run main.go

# 指定配置文件
go run main.go -config=config/config.dev.json
go run main.go -c config/config.prod.json
```

### 2. 启动脚本

**Windows:**
```cmd
start.bat                    # 默认配置
start.bat dev                # 开发环境
start.bat prod               # 生产环境
start.bat custom my.json     # 自定义配置
start.bat help               # 显示帮助
```

**Linux/Mac:**
```bash
./start.sh                   # 默认配置
./start.sh dev               # 开发环境
./start.sh prod              # 生产环境
./start.sh custom my.json    # 自定义配置
./start.sh help              # 显示帮助
```

## 环境配置建议

### 开发环境 (config.dev.json)
- 使用debug模式
- 数据库名称添加_dev后缀
- JWT密钥可以使用简单的字符串
- Token过期时间可以设置较长

### 生产环境 (config.prod.json)
- 使用release模式
- 使用独立的生产数据库
- JWT密钥必须使用强密码
- Token过期时间建议设置较短
- 数据库用户权限最小化

## 安全注意事项

1. **不要将生产环境配置文件提交到版本控制系统**
2. **JWT密钥必须保密且足够复杂**
3. **数据库密码应该定期更换**
4. **生产环境建议使用环境变量覆盖敏感配置**

## 配置文件优先级

1. 命令行指定的配置文件
2. 默认配置文件 (config.json)

## 故障排除

### 配置文件不存在
```
错误: 无法打开配置文件 config/missing.json: no such file or directory
```
**解决方案**: 检查配置文件路径是否正确

### 配置文件格式错误
```
错误: 解析配置文件失败 config/config.json: invalid character '}' looking for beginning of object key string
```
**解决方案**: 检查JSON格式是否正确，可以使用JSON验证工具

### 数据库连接失败
```
错误: 数据库连接失败: Error 1045: Access denied for user 'root'@'localhost'
```
**解决方案**: 检查数据库配置信息是否正确