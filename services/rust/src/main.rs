use std::convert::Infallible;
use warp::Filter;
use serde::Serialize;
use std::io::Read;

#[derive(Serialize)]
struct TrackSummary {
    points: usize,
    distance_m: f64,
    elevation_gain_m: f64,
}

#[tokio::main]
async fn main() {
    // POST /process-gpx expects multipart/form-data with file field "gpx"
    let process = warp::post()
        .and(warp::path("process-gpx"))
        .and(warp::multipart::form().max_length(10_000_000))
        .and_then(handle_gpx);

    println!("Rust GPX processor running on http://127.0.0.1:3030");
    warp::serve(process).run(([127,0,0,1], 3030)).await;
}

async fn handle_gpx(form: warp::multipart::FormData) -> Result<impl warp::Reply, Infallible> {
    use futures::TryStreamExt;
    let parts: Vec<_> = form.try_collect().await.unwrap_or_default();
    for part in parts {
        if part.name() == "gpx" {
            let content = part.stream().try_fold(Vec::new(), |mut acc, bytes| async move {
                acc.extend_from_slice(&bytes);
                Ok(acc)
            }).await.unwrap_or_default();

            let mut reader = std::io::Cursor::new(content);
            let mut s = String::new();
            reader.read_to_string(&mut s).ok();

            // parse GPX
            match gpx::read(&mut s.as_bytes()) {
                Ok(gpx) => {
                    let mut points = 0usize;
                    let mut distance_m = 0f64;
                    let mut elev_gain = 0f64;
                    let mut prev: Option<(f64,f64,f64)> = None;

                    for track in &gpx.tracks {
                        for segment in &track.segments {
                            for p in &segment.points {
                                points += 1;
                                let lat = p.point().lat();
                                let lon = p.point().lng();
                                let ele = p.elevation.unwrap_or(0.0);
                                if let Some((plat, plon, pele)) = prev {
                                    distance_m += haversine_m(plat, plon, lat, lon);
                                    let diff = ele - pele;
                                    if diff > 0.0 { elev_gain += diff; }
                                }
                                prev = Some((lat, lon, ele));
                            }
                        }
                    }

                    let summary = TrackSummary {
                        points,
                        distance_m,
                        elevation_gain_m: elev_gain,
                    };
                    // Primary (fast) summary produced by Rust.
                    // Wrap it inside a "primary" field and provide a placeholder "secondary": null
                    // so downstream workers know primary finished first.
                    let primary_json = serde_json::to_string(&summary).unwrap_or_else(|_| "{}".into());
                    let combined = format!("{{\"primary\": {}, \"secondary\": null}}", primary_json);
                    return Ok(warp::reply::with_header(combined, "Content-Type", "application/json"));
                }
                Err(_) => {
                    return Ok(warp::reply::with_status(
                        "Failed to parse GPX",
                        warp::http::StatusCode::BAD_REQUEST,
                    ));
                }
            }
        }
    }

    Ok(warp::reply::with_status(
        "No GPX file provided",
        warp::http::StatusCode::BAD_REQUEST,
    ))
}

fn haversine_m(lat1: f64, lon1: f64, lat2: f64, lon2: f64) -> f64 {
    let rad = std::f64::consts::PI / 180.0;
    let dlat = (lat2 - lat1) * rad;
    let dlon = (lon2 - lon1) * rad;
    let a = (dlat / 2.0).sin().powi(2)
        + lat1.to_radians().cos() * lat2.to_radians().cos() * (dlon / 2.0).sin().powi(2);
    let c = 2.0 * a.sqrt().asin();
    let earth = 6_371_000.0;
    earth * c
}