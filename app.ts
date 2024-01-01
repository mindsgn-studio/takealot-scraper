import { clientPromise } from "./utility";
import { searchItems } from "./constants";
import fetch, { Headers } from "node-fetch";
import "dotenv/config";

let random: string;

const sleep = (millis: number) => {
  return new Promise((resolve) => setTimeout(resolve, millis));
};

const getTakealotIDFromLink = (link: string) => {
  const regex = /PLID(\d+)/;
  const match = link.match(regex);

  return match ? match[1] : null;
};

const extractGalleryImages = (images: [string]) => {
  if (Array.isArray(images)) {
    const processedImages = [];

    for (const image of images) {
      if (typeof image === "string") {
        const processedImageUrl = image.replace(/{size}/g, "zoom");
        processedImages.push(processedImageUrl);
      } else {
        throw new Error("Image URL is not a string");
      }
    }

    return processedImages;
  } else {
    throw new Error("Images are not in the expected format");
  }
};

const searchTakealotProduct = async (search: string, nextIsAfter?: string) => {
  let apiURL = `https://api.takealot.com/rest/v-1-11-0/searches/products?newsearch=true&qsearch=${search}&track=1&userinit=true&searchbox=true`;

  if (nextIsAfter) {
    apiURL = `https://api.takealot.com/rest/v-1-11-0/searches/products?newsearch=true&qsearch=${search}&track=1&userinit=true&searchbox=true&after=${nextIsAfter}`;
  }

  const headers = new Headers({
    "User-Agent":
      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
    "Accept-Language": "en-US,en;q=0.9",
    Referer: "https://takealot.com",
  });

  const requestOptions = {
    method: "GET",
    headers: headers,
  };

  const fetchResponse = await fetch(apiURL, requestOptions)
    .then((response) => {
      if (!response.ok) {
        throw new Error(`HTTP error! Status: ${response.status}`);
      }
      return response.json();
    })
    .then(async (response) => {
      const { sections } = response;
      const { products } = sections;
      const { results, paging, is_paged } = products;
      const { next_is_after } = paging;
      nextIsAfter = next_is_after;

      results.map(async (item) => {
        const { product_views } = item;

        const {
          core,
          stock_availability_summary,
          gallery,
          buybox_summary,
          enhanced_ecommerce_click,
        } = product_views;

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

        const client = await clientPromise;
        const db = await client.db(`${process.env.MONGODB_DATABASE}`);
        const options = { upsert: true };
        const cursor = await db
          .collection("items")
          .updateOne(filter, update, options);
        const { matchedCount, upsertedCount } = cursor;
        if (matchedCount == 0) {
          console.log(`new item: ${title}`, upsertedCount, matchedCount);
        } else {
          console.log(`updated item: ${title}`, upsertedCount, matchedCount);
        }
      });

      console.log(search, nextIsAfter, is_paged);

      if (nextIsAfter != "") {
        await sleep(5000);
        searchTakealotProduct(search, nextIsAfter);
      } else {
        random = getRandom().toFixed();
        searchTakealotProduct(searchItems[random]);
      }
    })
    .catch(async () => {
      random = getRandom().toFixed();
      searchTakealotProduct(searchItems[random]);
    });

  return fetchResponse;
};

const getRandom = () => {
  return Math.random() * (searchItems.length - 1);
};

random = getRandom().toFixed();

searchTakealotProduct(searchItems[random]);
