"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.clientPromise = void 0;
const mongodb_1 = require("mongodb");
require("dotenv/config");
const uri = `${process.env.MONGODB_URI}`;
let client;
let clientPromise;
if (!uri) {
    throw new Error("Please add your Mongo URI to .env.local");
}
if (process.env.NODE_ENV === "development") {
    //@ts-ignore
    if (!global._mongoClientPromise) {
        //@ts-ignore
        client = new mongodb_1.MongoClient(uri);
        //@ts-ignore
        global._mongoClientPromise = client.connect();
    }
    //@ts-ignore
    exports.clientPromise = clientPromise = global._mongoClientPromise;
}
else {
    //@ts-ignore
    client = new mongodb_1.MongoClient(uri);
    exports.clientPromise = clientPromise = client.connect();
}
//# sourceMappingURL=database.js.map