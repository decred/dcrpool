$.fn["pagination"].defaults.locator = "data";
$.fn["pagination"].defaults.totalNumberLocator = function(response) { return response.count; };
$.fn["pagination"].defaults.nextText = '<div class="pagination-arrow pagination-arrow-right"></div>';
$.fn["pagination"].defaults.prevText = '<div class="pagination-arrow pagination-arrow-left"></div>';
$.fn["pagination"].defaults.hideWhenLessThanOnePage = true;

if ($('#pending-payments-page-select').length) {
    $('#pending-payments-page-select').pagination({
        dataSource: "/admin/payments/pending",
        callback: function (data) {
            var html = '';
            if (data.length > 0) {
                $.each(data, function (_, item) {
                    html += '<tr><td><a href="' +  item.workheighturl + '" rel="noopener noreferrer">' +  item.workheight + '</a></td><td>' +  item.estimatedpaymentheight + '</td><td>' +  item.amount + '</td></tr>';
                });
            } else {
                html += '<tr><td colspan="100%"><span class="no-data">No pending payments</span></td></tr>';
            }
            $('#pending-payments-table').html(html);
        }
    });
};

if ($('#archived-payments-page-select').length) {
    $('#archived-payments-page-select').pagination({
        dataSource: "/admin/payments/archived",
        callback: function (data) {
            var html = '';
            if (data.length > 0) {
                $.each(data, function (_, item) {
                    html += '<tr><td><a href="' +  item.workheighturl + '" rel="noopener noreferrer">' +  item.workheight + '</a></td><td><a href="' +  item.paidheighturl + '" rel="noopener noreferrer">' +  item.paidheight + '</a></td><td>' +  item.amount + '</td><td><a href="' +  item.txurl + '" rel="noopener noreferrer">' +  item.txid + '</a></td></tr>';
                });
            } else {
                html += '<tr><td colspan="100%"><span class="no-data">No received payments</span></td></tr>';
            }
            $('#archived-payments-table').html(html);
        }
    });
};