import os

ES_URL = os.getenv("ES_URL", "http://localhost:9200")
MONGO_URL = os.getenv("MONGO_URL", "mongodb://localhost:27017")
MONGO_DB = os.getenv("MONGO_DB", "forge")
MYSQL_URL = os.getenv("MYSQL_URL", "mysql+pymysql://forge:forge@localhost:3306/forge")
PORT = int(os.getenv("PORT", "8090"))

INDEX = "documents"
EMBED_MODEL = "BAAI/bge-small-en-v1.5"
EMBED_DIMS = 384
