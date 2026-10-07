-- OpenAI OAuth 提供商使用创建表单或导入文件中的配置，清理已停用的全局模板。
DELETE FROM settings WHERE key = 'openai_oauth_import_defaults';
