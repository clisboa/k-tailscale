#pragma once
#include <QImage>
#include <QString>
namespace tailui {
// Null means the full text URL must remain available as the fallback.
QImage loginQr(const QString &url, int maxPixels);
}
