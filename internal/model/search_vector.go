package model

const CodeChunkSearchVectorExpression = `to_tsvector('simple',
	coalesce(file_path, '') || ' ' ||
	coalesce(symbol_name, '') || ' ' ||
	coalesce(symbol_type, '') || ' ' ||
	coalesce(language, '') || ' ' ||
	coalesce(content, '')
)`
