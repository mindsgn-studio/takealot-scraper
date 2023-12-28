"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || function (mod) {
    if (mod && mod.__esModule) return mod;
    var result = {};
    if (mod != null) for (var k in mod) if (k !== "default" && Object.prototype.hasOwnProperty.call(mod, k)) __createBinding(result, mod, k);
    __setModuleDefault(result, mod);
    return result;
};
var __awaiter = (this && this.__awaiter) || function (thisArg, _arguments, P, generator) {
    function adopt(value) { return value instanceof P ? value : new P(function (resolve) { resolve(value); }); }
    return new (P || (P = Promise))(function (resolve, reject) {
        function fulfilled(value) { try { step(generator.next(value)); } catch (e) { reject(e); } }
        function rejected(value) { try { step(generator["throw"](value)); } catch (e) { reject(e); } }
        function step(result) { result.done ? resolve(result.value) : adopt(result.value).then(fulfilled, rejected); }
        step((generator = generator.apply(thisArg, _arguments || [])).next());
    });
};
Object.defineProperty(exports, "__esModule", { value: true });
const utility_1 = require("./utility");
const constants_1 = require("./constants");
const node_fetch_1 = __importStar(require("node-fetch"));
require("dotenv/config");
const sleep = (millis) => {
    return new Promise((resolve) => setTimeout(resolve, millis));
};
const getTakealotIDFromLink = (link) => {
    const regex = /PLID(\d+)/;
    const match = link.match(regex);
    return match ? match[1] : null;
};
const extractGalleryImages = (images) => {
    if (Array.isArray(images)) {
        const processedImages = [];
        for (const image of images) {
            if (typeof image === "string") {
                const processedImageUrl = image.replace(/{size}/g, "zoom");
                processedImages.push(processedImageUrl);
            }
            else {
                throw new Error("Image URL is not a string");
            }
        }
        return processedImages;
    }
    else {
        throw new Error("Images are not in the expected format");
    }
};
const searchTakealotProduct = (search, nextIsAfter) => __awaiter(void 0, void 0, void 0, function* () {
    let apiURL = `https://api.takealot.com/rest/v-1-11-0/searches/products?newsearch=true&qsearch=${search}&track=1&userinit=true&searchbox=true`;
    if (nextIsAfter) {
        apiURL = `https://api.takealot.com/rest/v-1-11-0/searches/products?newsearch=true&qsearch=${search}&track=1&userinit=true&searchbox=true&after=${nextIsAfter}`;
    }
    const headers = new node_fetch_1.Headers({
        "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
        "Accept-Language": "en-US,en;q=0.9",
        Referer: "https://takealot.com",
    });
    const requestOptions = {
        method: "GET",
        headers: headers,
    };
    const fetchResponse = yield (0, node_fetch_1.default)(apiURL, requestOptions)
        .then((response) => {
        if (!response.ok) {
            throw new Error(`HTTP error! Status: ${response.status}`);
        }
        return response.json();
    })
        .then((response) => __awaiter(void 0, void 0, void 0, function* () {
        const { sections } = response;
        const { products } = sections;
        const { results, paging, is_paged } = products;
        const { next_is_after } = paging;
        nextIsAfter = next_is_after;
        results.map((item) => __awaiter(void 0, void 0, void 0, function* () {
            const { product_views } = item;
            const { core, stock_availability_summary, gallery, buybox_summary, enhanced_ecommerce_click, } = product_views;
            const { ecommerce } = enhanced_ecommerce_click;
            const { click } = ecommerce;
            const { products } = click;
            const { id } = products[0];
            const newID = getTakealotIDFromLink(id);
            const { brand, title, slug } = core;
            const { status } = stock_availability_summary;
            const { images } = gallery;
            const { prices } = buybox_summary;
            const price = prices[0];
            const newImages = extractGalleryImages(images);
            const now = new Date();
            const filter = {
                "sources.id": newID,
            };
            const update = {
                $push: {
                    prices: {
                        $each: [
                            {
                                date: now,
                                price: price,
                            },
                        ],
                    },
                },
                $set: {
                    title,
                    images: newImages,
                    brand,
                    status,
                    link: `https://www.takealot.com/${slug}/PLID${newID}`,
                    updated: new Date(),
                    sources: {
                        id: newID,
                        source: "takealot",
                        api: `https://api.takealot.com/rest/v-1-11-0/product-details/PLID${newID}?platform=desktop&display_credit=true`,
                    },
                },
            };
            const client = yield utility_1.clientPromise;
            const db = yield client.db(`${process.env.MONGODB_DATABASE}`);
            const options = { upsert: true };
            const cursor = yield db
                .collection("items")
                .updateOne(filter, update, options);
            const { matchedCount, upsertedCount } = cursor;
            if (matchedCount == 0) {
                console.log(`new item: ${title}`, upsertedCount, matchedCount);
            }
            else {
                console.log(`updated item: ${title}`, upsertedCount, matchedCount);
            }
        }));
        console.log(search, nextIsAfter, is_paged);
        if (nextIsAfter != "") {
            yield sleep(5000);
            searchTakealotProduct(search, nextIsAfter);
        }
    }))
        .catch((error) => __awaiter(void 0, void 0, void 0, function* () {
        console.log(error);
    }));
    return fetchResponse;
});
const getRandom = () => {
    return Math.random() * (constants_1.searchItems.length - 1);
};
const random = getRandom().toFixed();
searchTakealotProduct(constants_1.searchItems[random]);
//# sourceMappingURL=app.js.map