import { socialImage } from "@/lib/social-image";

export const alt = "Cubeship — Your infrastructure. Ready to ship.";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

export default function Image() {
  return socialImage({
    title: "Your infrastructure. Ready to ship.",
    description: "The power of a cloud platform. The freedom of your own servers.",
  });
}
