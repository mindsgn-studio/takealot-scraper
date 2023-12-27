import { clientPromise } from "./utility";
import { ObjectId } from "mongodb";
import fetch, { Headers } from "node-fetch";
import "dotenv/config";

let previousTime: Date = new Date();

const has12HourPassed = (previousTime: Date) => {
  const currentTime: Date = new Date();
  //@ts-ignore
  const timeDifference = currentTime - previousTime;
  const hoursDifference = timeDifference / (1000 * 60 * 60);
  return hoursDifference >= 1;
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

const isLink = (link: string) => {
  const regex = /PLID(\d+)/;
  const match = link.match(regex);

  return match ? link : null;
};

const updateTakealotItem = async (api: string, id: string, link: string) => {
  const apiURL = `${api}`;

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

  await fetch(apiURL, requestOptions)
    .then(async (response) => {
      if (!response.ok) {
        throw new Error(`HTTP error! Status: ${response.status}`);
      }
      return response.json();
    })
    .then(async (response) => {
      console.log(response);
      const { title, core, stock_availability, gallery, buybox } = response;
      const { brand } = core;
      const { status } = stock_availability;
      const { images } = gallery;
      const { prices } = buybox;
      const price = prices[0];
      const newImages = extractGalleryImages(images);
      const now = new Date();

      const filter = {
        title,
      };

      const client = await clientPromise;
      const db = await client.db(`${process.env.MONGO_DB}`);
      const options = { upsert: true };

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
          link,
          updated: now,
          sources: {
            id,
            source: "takealot",
            api: `https://api.takealot.com/rest/v-1-11-0/product-details/PLID${id}?platform=desktop&display_credit=true`,
          },
        },
      };

      await db.collection("items").updateOne(filter, update, options);
    })
    .catch(() => {
      return null;
    });
};

const getID = async (id: string) => {
  const client = await clientPromise;
  const db = await client.db(`${process.env.MONGODB_DATABASE}`);
  const cursor = await db
    .collection("items")
    .find({ _id: new ObjectId(id) })
    .toArray();

  cursor.map((item: any) => {
    const { sources, link } = item;
    const { source, api, id } = sources;
    if (source === "takealot") {
      if (isLink(api)) {
        updateTakealotItem(api, id, link);
      }
    }
  });
};

const getWishlist = async () => {
  const client = await clientPromise;
  const db = await client.db(`${process.env.MONGODB_DATABASE}`);
  const cursor = await db.collection("watchlist").find({}).toArray();

  cursor.map((item) => {
    const { id } = item;
    getID(id);
  });
};

setInterval(() => {
  getWishlist();
}, 10000);
